package executionboundary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePackName(t *testing.T) {
	for _, name := range []string{"default", "python-3.11", "team_pack.v2"} {
		if err := ValidatePackName(name); err != nil {
			t.Fatalf("valid name %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", "../escape", "/absolute", "--flag", "name/child", "name with space", strings.Repeat("a", 129)} {
		if err := ValidatePackName(name); err == nil {
			t.Fatalf("unsafe name %q accepted", name)
		}
	}
}

func TestCommandAllowsOnlyFixedBuildTools(t *testing.T) {
	if command, err := Command("sh", "-c", "touch should-not-run"); err == nil || command != nil {
		t.Fatal("shell executable was permitted")
	}

	bin := t.TempDir()
	docker := filepath.Join(bin, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	args := []string{"buildx", "build"}
	command, err := Command("docker", args...)
	if err != nil {
		t.Fatalf("permitted executable rejected: %v", err)
	}
	args[0] = "mutated"
	if command.Path != docker || command.Args[1] != "buildx" {
		t.Fatalf("command was not resolved and copied safely: path=%q args=%v", command.Path, command.Args)
	}
}

func TestPrepareOutputDirectoryConfinesPaths(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{"../outside", "/tmp/absolute", "escape/child"} {
		if _, err := PrepareOutputDirectory(workspace, candidate); err == nil {
			t.Fatalf("unsafe output %q accepted", candidate)
		}
	}
	valid, err := PrepareOutputDirectory(workspace, "build/runtime")
	if err != nil {
		t.Fatalf("valid output rejected: %v", err)
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if valid != filepath.Join(resolvedWorkspace, "build", "runtime") {
		t.Fatalf("resolved output = %q", valid)
	}
	current, err := PrepareOutputDirectory(workspace, ".")
	if err != nil {
		t.Fatalf("workspace output rejected: %v", err)
	}
	if current != resolvedWorkspace {
		t.Fatalf("workspace output = %q, want %q", current, resolvedWorkspace)
	}
}

func TestWriteFileRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "requirements.txt")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(root, "requirements.txt", []byte("replacement"), 0o600); err == nil {
		t.Fatal("rooted write followed an escaping symlink")
	}
	contents, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "original" {
		t.Fatalf("outside file was modified: %q", contents)
	}
}

func TestReadFileRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "pack.yml")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "pack.yml")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../pack.yml", outside, "pack.yml"} {
		if _, err := ReadFile(root, name); err == nil {
			t.Fatalf("unsafe rooted read %q succeeded", name)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "valid.yml"), []byte("valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(root, "valid.yml")
	if err != nil || string(data) != "valid" {
		t.Fatalf("valid rooted read = %q, %v", data, err)
	}
}
