package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchProjectAtImmutableCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	source := t.TempDir()
	runGitForTest(t, source, "init")
	runGitForTest(t, source, "config", "user.email", "praetor-test@example.invalid")
	runGitForTest(t, source, "config", "user.name", "Praetor Test")
	if err := os.WriteFile(filepath.Join(source, "playbook.yml"), []byte("---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, source, "add", "playbook.yml")
	runGitForTest(t, source, "commit", "-m", "fixture")
	commit := strings.TrimSpace(runGitForTest(t, source, "rev-parse", "HEAD"))

	destination := filepath.Join(t.TempDir(), "checkout")
	if err := fetchProject(source, commit, destination); err != nil {
		t.Fatalf("fetch immutable commit: %v", err)
	}
	if got := strings.TrimSpace(runGitForTest(t, destination, "rev-parse", "HEAD")); got != commit {
		t.Fatalf("checked out commit = %s, want %s", got, commit)
	}
	if _, err := os.Stat(filepath.Join(destination, "playbook.yml")); err != nil {
		t.Fatalf("checked-out project is missing its file: %v", err)
	}
}

func TestIsGitCommitID(t *testing.T) {
	for _, ref := range []string{strings.Repeat("a", 40), strings.Repeat("b", 64)} {
		if !isGitCommitID(ref) {
			t.Fatalf("valid commit ID %q was rejected", ref)
		}
	}
	for _, ref := range []string{"main", strings.Repeat("z", 40), strings.Repeat("a", 39)} {
		if isGitCommitID(ref) {
			t.Fatalf("non-commit ref %q was accepted", ref)
		}
	}
}

func runGitForTest(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}
