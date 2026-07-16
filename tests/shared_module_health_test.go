package tests

import (
	"os"
	"strings"
	"testing"
)

func TestSharedModuleHealthContract(t *testing.T) {
	script, err := os.ReadFile("../scripts/check-workspace-health.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	required := []string{
		"-output shared-modules",
		"GOWORK=off",
		"gofmt -l",
		"go \"$check\" ./...",
		"clone --quiet --depth 1 --branch \"$version\"",
		"security-tests",
		"--modules",
		"--remote",
	}
	for _, item := range required {
		if !strings.Contains(text, item) {
			t.Errorf("shared-module health checker is missing %q", item)
		}
	}
}

func TestReleasePreflightIncludesSharedModuleHealth(t *testing.T) {
	script, err := os.ReadFile("../scripts/release-preflight.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "check-workspace-health.sh --modules --remote") {
		t.Fatal("remote release preflight does not execute released shared-module health")
	}
}
