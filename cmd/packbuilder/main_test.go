package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildPackRejectsUnsafeNameBeforeProcessExecution(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "executed")
	buildctl := filepath.Join(bin, "buildctl")
	script := "#!/bin/sh\ntouch \"$EXECUTION_MARKER\"\n"
	if err := os.WriteFile(buildctl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("EXECUTION_MARKER", marker)

	validSpec := "name: safe\npython: 3.11.9\nansible_core: 2.19.11\narches: [arm64]\nhost_runner: v0.5.0\n"
	for _, name := range []string{"../escape", "/absolute", "--output=evil", "nested/name"} {
		if _, err := buildPack(name, validSpec); err == nil {
			t.Fatalf("unsafe name %q accepted", name)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("build process executed for rejected input")
	}
}
