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
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec is the Execution Pack definition.
type Spec struct {
	Name        string   `yaml:"name"`        // output pack name -> <name>-linux-<arch>.tar.gz
	Python      string   `yaml:"python"`      // standalone CPython version, e.g. "3.11.9"
	Ansible     string   `yaml:"ansible"`     // pip requirement: "ansible", "ansible-core==2.16.*"
	Pip         []string `yaml:"pip"`         // extra pip packages (module deps: docker, jmespath, boto3, ...)
	Collections []string `yaml:"collections"` // extra ansible-galaxy collections
	Arches      []string `yaml:"arches"`      // target CPU arches: arm64, amd64
	Libc        []string `yaml:"libc"`        // target C libs: gnu (glibc), musl (alpine); default [gnu]
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
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		log.Fatalf("parse spec %s: %v", *specPath, err)
	}

	// Defaults.
	if spec.Name == "" {
		spec.Name = "execpack"
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
	if len(spec.Libc) == 0 {
		spec.Libc = []string{"gnu"}
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("create out dir: %v", err)
	}

	for _, arch := range spec.Arches {
		for _, libc := range spec.Libc {
			// musl is gated off: python-build-standalone's musl CPython is static and
			// can't dlopen Ansible's C extensions ("Dynamic loading not supported"),
			// and has no aarch64 build. Plumbing stays for a future dynamic musl python.
			if libc == "musl" {
				log.Printf("skipping %s+musl: PBS musl python is static and can't load Ansible's C extensions", arch)
				continue
			}
			suf, baseImage := "", "debian:12-slim"
			if libc == "musl" {
				suf, baseImage = "-musl", "alpine:3.19"
			}
			log.Printf("Building Execution Pack %q for linux/%s (%s) (python %s, ansible %q, %d collections, %d pip)...",
				spec.Name, arch, libc, spec.Python, spec.Ansible, len(spec.Collections), len(spec.Pip))
			args := []string{
				"buildx", "build",
				"--platform", "linux/" + arch,
				"--build-arg", "BUILD_IMAGE=" + baseImage,
				"--build-arg", "TARGETARCH=" + arch,
				"--build-arg", "LIBC=" + libc,
				"--build-arg", "PY_VERSION=" + spec.Python,
				"--build-arg", "ANSIBLE_SPEC=" + spec.Ansible,
				"--build-arg", "EXTRA_PIP=" + strings.Join(spec.Pip, " "),
				"--build-arg", "GALAXY_COLLECTIONS=" + strings.Join(spec.Collections, " "),
				"--build-arg", "PACK_NAME=" + spec.Name,
				"--target", "export",
				"-o", "type=local,dest=" + *out,
				"build/ansible-runtime",
			}
			cmd := exec.Command("docker", args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Fatalf("docker build for %s/%s failed: %v", arch, libc, err)
			}
			fmt.Printf("  -> %s/%s-linux-%s%s.tar.gz\n", *out, spec.Name, arch, suf)
		}
	}
	log.Printf("Execution Pack %q built.", spec.Name)
}
