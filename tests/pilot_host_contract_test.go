package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPilotHostIsIsolatedAndKeyOnly(t *testing.T) {
	root := repositoryRoot(t)
	read := func(path string) string {
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	script := read("scripts/pilot-host.sh")
	dockerfile := read("deployments/pilot-host/Dockerfile")
	sshd := read("deployments/pilot-host/sshd_config")

	for _, forbidden := range []string{"--privileged", "-p $", "docker.sock", "/root/.ssh", "PermitRootLogin yes", "PasswordAuthentication yes"} {
		if strings.Contains(script+dockerfile+sshd, forbidden) {
			t.Fatalf("pilot target contains forbidden exposure %q", forbidden)
		}
	}
	for _, required := range []string{
		"--read-only", "--cap-drop ALL", "--cap-add AUDIT_WRITE", "--cap-add CHOWN", "--cap-add KILL", "--cap-add SYS_CHROOT", "PermitRootLogin no", "PasswordAuthentication no",
		"AllowUsers praetor", "id_ed25519.pub:/run/praetor/authorized_keys:ro",
		"k3d-praetor-staging-", "timeout 3 telnet", "rockylinux:9@sha256:", "StrictHostKeyChecking=yes",
		"UserKnownHostsFile=/run/praetor/known_hosts", "praetor@$TARGET", "root@$TARGET", ".HostConfig.Privileged == false",
		"desired_image=", "docker inspect \"$TARGET\" --format '{{.Image}}'",
	} {
		if !strings.Contains(script+dockerfile+sshd, required) {
			t.Fatalf("pilot target is missing security contract %q", required)
		}
	}
}

func TestPilotHostResetIsBounded(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "pilot-host.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, forbidden := range []string{"k3d cluster delete", "kubectl delete namespace", "docker system prune", "staging/storage", "staging/recovery"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("pilot reset must not contain %q", forbidden)
		}
	}
}
