package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
)

const (
	// Fixed prefix the self-contained runtime is laid out at (its console-script
	// shebangs are baked to this path, so it must extract here).
	runtimePrefix = "/opt/praetor/runtime"
	// Where the executor's bootstrap pushed the runtime tarball.
	runtimeTarball = "/opt/praetor/ansible-runtime.tar.gz"
)

// resolveAnsible returns the ansible-playbook command and Python interpreter the
// host-runner should use. Praetor's self-contained execution environment
// (Python + Ansible) is *pushed* onto the host by the executor at
// runtimeTarball; this extracts it once under runtimePrefix and returns the
// bundled binaries, so the host needs no pre-installed Ansible or Python and
// everything stays under /opt/praetor. If no runtime was pushed (e.g. a musl
// host, for which there is no glibc bundle), it falls back to a system
// ansible-playbook on PATH.
func resolveAnsible() (playbook, interpreter string) {
	bundled := filepath.Join(runtimePrefix, "bin", "ansible-playbook")
	python := filepath.Join(runtimePrefix, "bin", "python3")

	if !fileExists(bundled) && fileExists(runtimeTarball) {
		if err := extractRuntime(); err != nil {
			log.Printf("runtime: extract failed (%v); falling back to system ansible-playbook", err)
		}
	}
	if fileExists(bundled) {
		// The ExecPack always provides the Ansible engine (controller). For module
		// execution on the host, prefer the host's own Python when it has one — so
		// modules needing system bindings (e.g. apt/python3-apt) work — and only
		// fall back to the ExecPack's Python on a host that has none.
		if hasSystemPython() {
			log.Printf("runtime: ExecPack Ansible engine + host system python for modules")
			return bundled, ""
		}
		log.Printf("runtime: ExecPack Ansible engine + bundled python (host has none)")
		return bundled, python
	}
	log.Printf("runtime: no ExecPack present; using system ansible-playbook")
	return "ansible-playbook", ""
}

// hasSystemPython reports whether the host has its own Python interpreter
// (distinct from the ExecPack's, which lives under /opt/praetor/runtime).
func hasSystemPython() bool {
	for _, p := range []string{"/usr/bin/python3", "/usr/bin/python", "/usr/local/bin/python3"} {
		if fileExists(p) {
			return true
		}
	}
	if _, err := exec.LookPath("python3"); err == nil {
		return true
	}
	return false
}

// extractRuntime unpacks the pushed runtime tarball at its fixed prefix. The
// tarball contains opt/praetor/runtime/..., so it extracts to /.
func extractRuntime() error {
	log.Printf("runtime: extracting self-contained Ansible from %s", runtimeTarball)
	cmd := exec.Command("tar", "-xzf", runtimeTarball, "-C", "/")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar: %v: %s", err, out)
	}
	return nil
}
