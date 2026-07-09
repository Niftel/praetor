// Command execpack builds an Execution Pack (a self-contained Python + Ansible
// runtime Praetor pushes onto hosts) from a declarative YAML spec — the ExecPack
// equivalent of ansible-builder. It drives the parameterised Dockerfile at
// build/ansible-runtime and writes one tarball per target architecture.
//
//	go run ./cmd/execpack -spec build/execpack/specs/default.yml -out build/runtime
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/praetordev/praetor/pkg/packspec"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	specPath := flag.String("spec", "", "path to the Execution Pack YAML spec")
	out := flag.String("out", "build/runtime", "output directory for the pack tarball(s)")
	flag.Parse()

	if *specPath == "" {
		log.Fatal("--spec is required (path to an Execution Pack YAML definition)")
	}
	data, err := os.ReadFile(*specPath)
	if err != nil {
		log.Fatalf("read spec: %v", err)
	}
	spec, err := packspec.Parse(string(data))
	if err != nil {
		log.Fatalf("parse spec %s: %v", *specPath, err)
	}

	// Defaults, then validate (see pkg/packspec — same rules the service enforces).
	if spec.Name == "" {
		spec.Name = "execpack"
	}
	if spec.Python == "" {
		spec.Python = "3.11.9"
	}
	if len(spec.Arches) == 0 {
		spec.Arches = []string{"arm64"}
	}
	if err := spec.Validate(); err != nil {
		log.Fatalf("invalid spec %s: %v", *specPath, err)
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("create out dir: %v", err)
	}

	// The engine + module deps, validated, written to the build context as a
	// requirements.txt the Dockerfile installs with `pip install -r`.
	requirements := strings.Join(spec.Requirements(), "\n") + "\n"

	for _, arch := range spec.Arches {
		ctx, err := os.MkdirTemp("", "execpack-")
		if err != nil {
			log.Fatalf("temp build context: %v", err)
		}
		if werr := os.WriteFile(filepath.Join(ctx, "requirements.txt"), []byte(requirements), 0o644); werr != nil {
			os.RemoveAll(ctx)
			log.Fatalf("write requirements.txt: %v", werr)
		}

		log.Printf("Building Execution Pack %q for linux/%s (python %s, %s, host-runner %s, %d pip)...",
			spec.Name, arch, spec.Python, spec.AnsibleRequirement(), spec.HostRunner, len(spec.Pip))
		args := []string{
			"buildx", "build",
			"--platform", "linux/" + arch,
			"-f", "build/ansible-runtime/Dockerfile",
			"--build-arg", "TARGETARCH=" + arch,
			"--build-arg", "PY_VERSION=" + spec.Python,
			"--build-arg", "PACK_NAME=" + spec.Name,
			// The daemon release to bundle is REQUIRED by the Dockerfile (no default);
			// it's single-sourced from the spec's host_runner field.
			"--build-arg", "HOST_RUNNER_VERSION=" + spec.HostRunner,
			"--build-arg", "GITEA_OWNER=" + envOr("GITEA_OWNER", "praetor"),
		}
		// GITEA_URL: the mirror the build pulls Python/wheels/host-runner from. The
		// Dockerfile defaults to the compose name (gitea-host:3000); override via env
		// for other setups (e.g. GITEA_URL=http://host.docker.internal:3002 to reach a
		// host-published Gitea from a local buildx build).
		if u := os.Getenv("GITEA_URL"); u != "" {
			args = append(args, "--build-arg", "GITEA_URL="+u)
		}
		args = append(args,
			"--target", "export",
			"-o", "type=local,dest="+*out,
			ctx,
		)
		cmd := exec.Command("docker", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		os.RemoveAll(ctx)
		if err != nil {
			log.Fatalf("docker build for %s failed: %v", arch, err)
		}
		fmt.Printf("  -> %s/%s-linux-%s.tar.gz\n", *out, spec.Name, arch)
	}
	log.Printf("Execution Pack %q built.", spec.Name)
}
