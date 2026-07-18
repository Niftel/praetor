package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestAnsibleRuntimeEnvUsesBoundedJobDirectory(t *testing.T) {
	jobDir := "/var/lib/praetor/jobs/test-run"
	env := ansibleRuntimeEnv(jobDir)
	for _, want := range []string{
		"ANSIBLE_FORCE_COLOR=1",
		"HOME=" + jobDir,
		"ANSIBLE_LOCAL_TEMP=" + filepath.Join(jobDir, ".ansible", "tmp"),
	} {
		if !slices.Contains(env, want) {
			t.Fatalf("runtime environment is missing %q: %v", want, env)
		}
	}
	if slices.Contains(env, "HOME=/root") {
		t.Fatal("runtime environment must not write Ansible state below /root")
	}
}
