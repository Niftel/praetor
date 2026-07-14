// Command packbuilder builds Execution Packs from their YAML spec. It polls the
// execution_packs table for pending packs, runs the parameterised Dockerfile via
// the Docker daemon, and publishes the resulting pack tarball to Gitea's generic
// package registry (the artifact store the executor pulls packs from over HTTP),
// then marks the pack ready or failed. This is what makes "define a pack from
// YAML in Praetor" actually produce the pack.
package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/praetordev/db"
	"github.com/praetordev/metrics"
	"github.com/praetordev/packspec"
)

func main() {
	log.Println("Execution Pack builder starting...")
	database, err := db.InitDB()
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	metrics.Serve("")

	for {
		var pack struct {
			ID        int64          `db:"id"`
			Name      string         `db:"name"`
			Spec      sql.NullString `db:"spec"`
			SCMURL    sql.NullString `db:"scm_url"`
			SCMBranch sql.NullString `db:"scm_branch"`
			SpecPath  sql.NullString `db:"spec_path"`
		}
		if err := database.Get(&pack, `SELECT id, name, spec, scm_url, scm_branch, spec_path FROM execution_packs WHERE status='pending' ORDER BY id LIMIT 1`); err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("Building Execution Pack %q (id %d)...", pack.Name, pack.ID)
		database.Exec(`UPDATE execution_packs SET status='building', build_log=NULL WHERE id=$1`, pack.ID)

		var pre string
		specYAML := pack.Spec.String
		// Git-backed pack: pull the spec from the repo so a push rebuilds the pushed
		// content. The fetched YAML becomes the pack's stored spec.
		if pack.SCMURL.String != "" {
			fetched, log_, err := fetchSpecFromGit(pack.SCMURL.String, pack.SCMBranch.String, pack.SpecPath.String)
			pre = log_
			if err != nil {
				database.Exec(`UPDATE execution_packs SET status='failed', build_log=$2 WHERE id=$1`, pack.ID, tail(pre+"\nGIT SYNC FAILED: "+err.Error(), 8000))
				log.Printf("Pack %q git sync failed: %v", pack.Name, err)
				continue
			}
			specYAML = fetched
			database.Exec(`UPDATE execution_packs SET spec=$2 WHERE id=$1`, pack.ID, specYAML)
		}

		out, berr := buildPack(pack.Name, specYAML)
		out = pre + out
		status := "ready"
		if berr != nil {
			status = "failed"
			out += "\n\nBUILD FAILED: " + berr.Error()
			log.Printf("Pack %q build failed: %v", pack.Name, berr)
		} else {
			log.Printf("Pack %q built.", pack.Name)
		}
		if _, err := database.Exec(`UPDATE execution_packs SET status=$1, build_log=$2 WHERE id=$3`, status, tail(out, 8000), pack.ID); err != nil {
			log.Printf("Pack %q: recording final status %q failed: %v", pack.Name, status, err)
		}
	}
}

// buildPack builds every arch of a pack from its YAML spec and extracts the
// tarball(s) into /build/runtime (shared with the executor).
func buildPack(name, specYAML string) (string, error) {
	spec, err := packspec.Parse(specYAML)
	if err != nil {
		return "", err
	}
	if spec.Python == "" {
		spec.Python = "3.11.9"
	}
	if len(spec.Arches) == 0 {
		spec.Arches = []string{"arm64"}
	}
	// Validate before building: every field must be a clean, typed value (versions,
	// package specs, known arches). This is the guard that keeps the build inputs —
	// which end up in a requirements.txt and in the artifact pushed to hosts — from
	// smuggling pip flags or shell metacharacters. Git-backed specs pass through
	// here too, so an untrusted repo can't inject a build.
	if err := spec.Validate(); err != nil {
		return "", fmt.Errorf("invalid spec: %w", err)
	}
	// The requirements the pack installs (ansible engine + module deps), each
	// already validated — written to a file and `pip install -r`, never shell-split.
	requirements := strings.Join(spec.Requirements(), "\n") + "\n"

	giteaOwner := envOr("GITEA_OWNER", "praetor")
	// Server-side base URL for publishing the built pack to Gitea's package
	// registry (in-cluster name). A write:package token enables publishing; without
	// one the builder falls back to the legacy shared runtime dir.
	giteaAPI := envOr("GITEA_INTERNAL_URL", "http://gitea-host:3000")
	giteaToken := os.Getenv("GITEA_TOKEN")
	// The daemon version is the pack.yml `host_runner` field — the single source of
	// truth, required and validated by spec.Validate() above (no silent default).
	hostRunnerVersion := spec.HostRunner

	// Builds run on a dedicated BuildKit daemon (no host Docker socket here, #46).
	// The build fetches Python/Ansible/host-runner from the in-cluster Gitea; RUN
	// steps share buildkitd's netns (--oci-worker-net=host, on praetor-net) so they
	// reach Gitea by IP, but BuildKit strips Docker's embedded DNS from RUN
	// resolv.conf — so resolve the names HERE (via our own Docker DNS) and pin them
	// as build `add-hosts`. Two names matter:
	//   - the in-cluster Gitea (GITEA_INTERNAL_URL host, e.g. gitea-host): the
	//     python/host-runner/pip-index fetches; and
	//   - the browser-facing Gitea (GITEA_URL host, e.g. gitea.localhost) that
	//     Gitea's package index emits as absolute wheel URLs, routed via Traefik.
	buildGiteaURL := giteaAPI
	addHosts := map[string]string{}
	if h := hostFromURL(buildGiteaURL); h != "" {
		if ip := dnsIP(h); ip != "" {
			addHosts[h] = ip
		}
	}
	if browser := hostFromURL(envOr("GITEA_URL", "")); strings.HasSuffix(browser, ".localhost") {
		if ip := dnsIP(envOr("TRAEFIK_CONTAINER", "traefik")); ip != "" {
			addHosts[browser] = ip
		}
	}

	// Comma-joined add-hosts opt for buildctl (a single --opt; repeating it would
	// let the last win).
	var addHostsOpt string
	if len(addHosts) > 0 {
		parts := make([]string, 0, len(addHosts))
		for h, ip := range addHosts {
			parts = append(parts, h+"="+ip)
		}
		addHostsOpt = strings.Join(parts, ",")
	}

	// buildctl global flags that pin mTLS to the daemon. When BUILDKIT_TLS_CACERT
	// is set (the docker-compose stack mounts the shared cert volume and points at
	// it), the client presents its cert and verifies the daemon's — closing the
	// otherwise-unauthenticated gRPC listener on praetor-net. Empty (e.g. a plain
	// local run) leaves the connection untouched. These are GLOBAL flags, so they
	// must precede the `build` subcommand.
	var tlsPrefix []string
	if ca := os.Getenv("BUILDKIT_TLS_CACERT"); ca != "" {
		tlsPrefix = append(tlsPrefix, "--tlscacert", ca)
		if c := os.Getenv("BUILDKIT_TLS_CERT"); c != "" {
			tlsPrefix = append(tlsPrefix, "--tlscert", c)
		}
		if k := os.Getenv("BUILDKIT_TLS_KEY"); k != "" {
			tlsPrefix = append(tlsPrefix, "--tlskey", k)
		}
		if sn := os.Getenv("BUILDKIT_TLS_SERVERNAME"); sn != "" {
			tlsPrefix = append(tlsPrefix, "--tlsservername", sn)
		}
	}

	var out strings.Builder
	for _, arch := range spec.Arches {
		// Per-build context holding only requirements.txt; the Dockerfile is a
		// separate --local dir. A temp dir avoids races between concurrent arch builds.
		ctx, err := os.MkdirTemp("", "packbuild-")
		if err != nil {
			return out.String(), fmt.Errorf("temp build context: %w", err)
		}
		if werr := os.WriteFile(filepath.Join(ctx, "requirements.txt"), []byte(requirements), 0644); werr != nil {
			os.RemoveAll(ctx)
			return out.String(), fmt.Errorf("write requirements.txt: %w", werr)
		}
		outDir, err := os.MkdirTemp("", "packout-")
		if err != nil {
			os.RemoveAll(ctx)
			return out.String(), fmt.Errorf("temp out dir: %w", err)
		}

		file := fmt.Sprintf("%s-linux-%s.tar.gz", name, arch)
		// One buildctl invocation builds AND extracts: the Dockerfile's `export`
		// stage (FROM scratch, COPY /out) is emitted with --output type=local, so
		// the tarball lands on disk directly — no docker create/cp/rm needed.
		args := append(append([]string{}, tlsPrefix...), "build",
			"--frontend", "dockerfile.v0",
			"--local", "context="+ctx,
			"--local", "dockerfile=/build/ansible-runtime",
			"--opt", "filename=Dockerfile",
			"--opt", "target=export",
			"--opt", "platform=linux/"+arch,
			"--opt", "build-arg:TARGETARCH="+arch,
			"--opt", "build-arg:PY_VERSION="+spec.Python,
			"--opt", "build-arg:PACK_NAME="+name,
			"--opt", "build-arg:GITEA_URL="+buildGiteaURL,
			"--opt", "build-arg:GITEA_OWNER="+giteaOwner,
			"--opt", "build-arg:HOST_RUNNER_VERSION="+hostRunnerVersion,
			"--output", "type=local,dest="+outDir,
		)
		if addHostsOpt != "" {
			args = append(args, "--opt", "add-hosts="+addHostsOpt)
		}
		// BUILDKIT_HOST (env, e.g. tcp://buildkitd:1234) points buildctl at the daemon.
		build := exec.Command("buildctl", args...)
		b, err := build.CombinedOutput()
		os.RemoveAll(ctx)
		out.Write(b)
		if err != nil {
			os.RemoveAll(outDir)
			return out.String(), fmt.Errorf("buildctl build (%s): %w", arch, err)
		}

		local := filepath.Join(outDir, file)
		if giteaToken != "" {
			// Publish to Gitea's generic registry — the artifact store the executor
			// pulls from over HTTP, decoupling it from this builder's filesystem.
			perr := publishPack(giteaAPI, giteaOwner, giteaToken, name, arch, local)
			os.RemoveAll(outDir)
			if perr != nil {
				return out.String(), fmt.Errorf("publish pack to Gitea: %w", perr)
			}
			out.WriteString(fmt.Sprintf("\npublished %s to %s/generic/execpack-%s@current\n", file, giteaOwner, name))
		} else {
			// No token: legacy behavior — copy into the shared runtime dir the
			// executor also mounts. (Set GITEA_TOKEN to publish to the registry.)
			if cerr := copyFile(local, filepath.Join("/build/runtime", file)); cerr != nil {
				os.RemoveAll(outDir)
				return out.String(), fmt.Errorf("copy pack to runtime dir: %w", cerr)
			}
			os.RemoveAll(outDir)
			out.WriteString(fmt.Sprintf("\nbuilt %s (shared dir; set GITEA_TOKEN to publish to Gitea)\n", file))
		}
	}
	return out.String(), nil
}

// publishPack uploads a built pack tarball to Gitea's generic package registry
// under execpack-<pack>/current/<pack>-linux-<arch>.tar.gz. The generic registry
// rejects re-upload of an existing file, so it deletes first — a rebuild refreshes
// the "current" artifact. Reads are anonymous; the executor pulls it over HTTP.
func publishPack(giteaURL, owner, token, pack, arch, localPath string) error {
	file := fmt.Sprintf("%s-linux-%s.tar.gz", pack, arch)
	dst := fmt.Sprintf("%s/api/packages/%s/generic/execpack-%s/current/%s",
		strings.TrimRight(giteaURL, "/"), owner, pack, file)

	// Best-effort delete so a rebuild can re-put the same path.
	if delReq, err := http.NewRequest(http.MethodDelete, dst, nil); err == nil {
		delReq.Header.Set("Authorization", "token "+token)
		if resp, err := http.DefaultClient.Do(delReq); err == nil {
			resp.Body.Close()
		}
	}

	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, dst, f)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = st.Size() // an *os.File body isn't auto-sized by net/http
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("gitea upload %s: HTTP %d: %s", file, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// fetchSpecFromGit shallow-clones the pack's repo/branch and returns the YAML at
// spec_path (plus a log of what it did). This is what makes a git push rebuild the
// pushed spec.
func fetchSpecFromGit(url, branch, specPath string) (string, string, error) {
	if specPath == "" {
		return "", "", fmt.Errorf("git-backed pack has no spec_path")
	}
	if branch == "" {
		branch = "main"
	}
	dir, err := os.MkdirTemp("", "packspec-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)

	var lg strings.Builder
	fmt.Fprintf(&lg, "git clone --depth=1 --branch %s %s\n", branch, url)
	b, err := exec.Command("git", "clone", "--depth=1", "--branch", branch, url, dir).CombinedOutput()
	lg.Write(b)
	if err != nil {
		return "", lg.String(), fmt.Errorf("git clone: %w", err)
	}
	// filepath.Clean("/"+specPath) prevents ../ escaping the checkout.
	full := filepath.Join(dir, filepath.Clean("/"+specPath))
	data, err := os.ReadFile(full)
	if err != nil {
		return "", lg.String(), fmt.Errorf("read %s: %w", specPath, err)
	}
	fmt.Fprintf(&lg, "read spec %s (%d bytes)\n", specPath, len(data))
	return string(data), lg.String(), nil
}

// envOr returns the env var k, or d when it is unset/empty.
func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// hostFromURL returns the hostname of a URL (e.g. http://gitea-host:3000 -> gitea-host).
func hostFromURL(u string) string {
	if p, err := url.Parse(u); err == nil && p.Hostname() != "" {
		return p.Hostname()
	}
	return u
}

// containerIP returns a container's IP on any of its networks (first non-empty),
// or "" if it can't be determined. Used to add an --add-host entry so the
// host-networked pack build can resolve the Gitea container by name whether it's
// on the default bridge or a user-defined compose network.
// dnsIP resolves a container/service name to an IP via the packbuilder's own
// Docker embedded DNS. Used to pin BuildKit `add-hosts` entries, since BuildKit
// strips Docker's embedded DNS from RUN resolv.conf but the resolved IPs are
// reachable from the build (buildkitd runs with --oci-worker-net=host on the same
// network). Replaces the old `docker inspect` lookup, which needed the host socket
// the packbuilder no longer mounts (#46).
func dnsIP(host string) string {
	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0]
}

// copyFile copies src to dst (used for the legacy shared-runtime-dir publish path).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// tail sanitizes build output for storage in the build_log TEXT column, then
// keeps the last n bytes. Docker/BuildKit progress output contains NUL bytes
// (and may contain invalid UTF-8); Postgres TEXT rejects both, which previously
// made the status UPDATE fail and left successful packs stuck at 'building'.
func tail(s string, n int) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "")
	if len(s) <= n {
		return s
	}
	return "...(truncated)...\n" + s[len(s)-n:]
}
