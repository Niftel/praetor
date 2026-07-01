package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
)

// defaultPack is used when a job doesn't name an Execution Pack.
const defaultPack = "ansible-runtime"

// resolveAnsible returns the ansible-playbook command and Python interpreter the
// host-runner should use for the given Execution Pack. Praetor's self-contained
// pack (Python + Ansible) is pushed onto the host by the executor at
// /opt/praetor/<pack>.tar.gz; this extracts it once under /opt/praetor/packs/<pack>
// and returns the bundled binaries, so the host needs no pre-installed Ansible or
// Python and everything stays under /opt/praetor. Packs are name-scoped so
// several can coexist on a host. If no pack was pushed (e.g. a musl host, for
// which there is no glibc pack), it falls back to a system ansible-playbook.
func resolveAnsible(pack string) (playbook, interpreter string) {
	if pack == "" {
		pack = defaultPack
	}
	prefix := "/opt/praetor/packs/" + pack
	tarball := "/opt/praetor/" + pack + ".tar.gz"
	bundled := filepath.Join(prefix, "bin", "ansible-playbook")
	python := filepath.Join(prefix, "bin", "python3")

	if !fileExists(bundled) && fileExists(tarball) {
		if err := extractPack(tarball); err != nil {
			log.Printf("runtime: extract of pack %q failed (%v); falling back to system ansible-playbook", pack, err)
		}
	}
	if fileExists(bundled) {
		// The pack always provides the Ansible engine. For module execution prefer
		// the host's own Python when it has one (so modules needing system bindings
		// like apt work), and fall back to the pack's Python on a bare host.
		if hasSystemPython() {
			log.Printf("runtime: Execution Pack %q (engine) + host system python for modules", pack)
			return bundled, ""
		}
		log.Printf("runtime: Execution Pack %q (engine + bundled python)", pack)
		return bundled, python
	}
	log.Printf("runtime: no Execution Pack %q present; using system ansible-playbook", pack)
	return "ansible-playbook", ""
}

// extractPack unpacks a pushed Execution Pack tarball at its fixed prefix. The
// tarball contains opt/praetor/packs/<pack>/..., so it extracts to /.
func extractPack(tarball string) error {
	log.Printf("runtime: extracting Execution Pack from %s", tarball)
	cmd := exec.Command("tar", "-xzf", tarball, "-C", "/")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar: %v: %s", err, out)
	}
	return nil
}

// hasSystemPython reports whether the host has its own Python interpreter
// (distinct from a pack's, which lives under /opt/praetor/packs).
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
