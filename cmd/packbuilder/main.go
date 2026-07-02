// Command packbuilder builds Execution Packs from their YAML spec. It polls the
// execution_packs table for pending packs, runs the parameterised Dockerfile via
// the Docker daemon, extracts the pack tarball into build/runtime/ (shared with
// the executor), and marks the pack ready or failed. This is what makes "define
// a pack from YAML in Praetor" actually produce the pack.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/praetordev/praetor/pkg/db"
	"gopkg.in/yaml.v3"
)

// Spec is the Execution Pack definition (mirrors cmd/execpack).
type Spec struct {
	Python      string   `yaml:"python"`
	Ansible     string   `yaml:"ansible"`
	Pip         []string `yaml:"pip"`
	Collections []string `yaml:"collections"`
	Arches      []string `yaml:"arches"`
}

func main() {
	log.Println("Execution Pack builder starting...")
	database, err := db.InitDB()
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

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
		database.Exec(`UPDATE execution_packs SET status=$1, build_log=$2 WHERE id=$3`, status, tail(out, 8000), pack.ID)
	}
}

// buildPack builds every arch of a pack from its YAML spec and extracts the
// tarball(s) into /build/runtime (shared with the executor).
func buildPack(name, specYAML string) (string, error) {
	var spec Spec
	if strings.TrimSpace(specYAML) != "" {
		if err := yaml.Unmarshal([]byte(specYAML), &spec); err != nil {
			return "", fmt.Errorf("parse spec: %w", err)
		}
	}
	if spec.Python == "" {
		spec.Python = "3.11.9"
	}
	if spec.Ansible == "" {
		spec.Ansible = "ansible"
	}
	if len(spec.Arches) == 0 {
		spec.Arches = []string{"arm64"}
	}

	// The pack pulls Python + wheels from the Gitea mirror. The build reaches
	// Gitea (a container) via --network=host + an --add-host entry resolving the
	// Gitea hostname to its bridge IP, so pip/curl inside the build can hit it.
	giteaURL := envOr("GITEA_URL", "http://gitea-host:3000")
	giteaOwner := envOr("GITEA_OWNER", "praetor")
	giteaHost := hostFromURL(giteaURL)
	hostRunnerVersion := envOr("HOST_RUNNER_VERSION", "v0.1.0")

	var out strings.Builder
	for _, arch := range spec.Arches {
		img := "praetor-execpack-" + name + "-" + arch
		args := []string{"build", "--target", "build",
			"--platform", "linux/" + arch,
			"--network", "host",
			"--build-arg", "TARGETARCH=" + arch,
			"--build-arg", "PY_VERSION=" + spec.Python,
			"--build-arg", "ANSIBLE_SPEC=" + spec.Ansible,
			"--build-arg", "EXTRA_PIP=" + strings.Join(spec.Pip, " "),
			"--build-arg", "GALAXY_COLLECTIONS=" + strings.Join(spec.Collections, " "),
			"--build-arg", "PACK_NAME=" + name,
			"--build-arg", "GITEA_URL=" + giteaURL,
			"--build-arg", "GITEA_OWNER=" + giteaOwner,
			"--build-arg", "HOST_RUNNER_VERSION=" + hostRunnerVersion,
		}
		if ip := bridgeIP(giteaHost); ip != "" {
			args = append(args, "--add-host", giteaHost+":"+ip)
		}
		args = append(args, "-t", img, "/build/ansible-runtime")
		build := exec.Command("docker", args...)
		// --platform needs buildkit for cross-arch (qemu) builds.
		build.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
		b, err := build.CombinedOutput()
		out.Write(b)
		if err != nil {
			return out.String(), fmt.Errorf("docker build (%s): %w", arch, err)
		}

		// Extract the pack tarball from the built image into the shared dir.
		cid, err := exec.Command("docker", "create", img).Output()
		if err != nil {
			return out.String(), fmt.Errorf("docker create: %w", err)
		}
		id := strings.TrimSpace(string(cid))
		if cp, err := exec.Command("docker", "cp", id+":/out/.", "/build/runtime/").CombinedOutput(); err != nil {
			out.Write(cp)
			return out.String(), fmt.Errorf("docker cp: %w", err)
		}
		exec.Command("docker", "rm", id).Run()
		exec.Command("docker", "rmi", img).Run()
		out.WriteString(fmt.Sprintf("\nbuilt %s-linux-%s.tar.gz\n", name, arch))
	}
	return out.String(), nil
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

// bridgeIP returns a container's IP on the default bridge network, or "" if it
// can't be determined. Used to add an --add-host entry so the (host-networked)
// pack build can resolve the Gitea container by name.
func bridgeIP(container string) string {
	out, err := exec.Command("docker", "inspect", "-f",
		`{{(index .NetworkSettings.Networks "bridge").IPAddress}}`, container).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "...(truncated)...\n" + s[len(s)-n:]
}
