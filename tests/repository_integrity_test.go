package tests

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestRepositoryHasNoUnmanagedGitlinks(t *testing.T) {
	cmd := exec.Command("git", "ls-files", "--stage")
	cmd.Dir = ".."
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("list tracked files: %v", err)
	}

	var gitlinks []string
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		if len(fields) >= 4 && fields[0] == "160000" {
			gitlinks = append(gitlinks, fields[3])
		}
	}

	if len(gitlinks) != 0 {
		t.Fatalf("unmanaged gitlinks are not allowed; use ordinary tracked files or a fully configured submodule: %s", strings.Join(gitlinks, ", "))
	}
}
