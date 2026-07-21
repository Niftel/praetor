package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionDiagnosticsStagingGateIsCompleteAndFailClosed(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-staging-execution-diagnostics.sh"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(raw) + readMakefile(t, root)
	for _, required := range []string{
		"staging-execution-diagnostics-preflight", "staging-execution-diagnostics-verify", "failed_task", "rejected_approval", "runner_bootstrap_failure",
		"control-plane interruption", "relaunch", "duplicates == 0", "gaps == 0", "cross_team_denied",
		"auditor_mutations_denied", "event_count >= 100", "api_p95_ms <= 750", "render_p95_ms <= 1500",
		"mobile_390x844", "PRAETOR_SECRET_CANARY", "sha256:[0-9a-f]{64}", "chmod 0600", "deployed API does not expose execution diagnostics",
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("staging diagnostics gate is missing %q", required)
		}
	}
}

func TestExecutionPackShipsDiagnosticsCallback(t *testing.T) {
	root := repositoryRoot(t)
	dockerfile, err := os.ReadFile(filepath.Join(root, "build", "ansible-runtime", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	execpack, err := os.ReadFile(filepath.Join(root, "cmd", "execpack", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := os.ReadFile(filepath.Join(root, "cmd", "host-runner", "plugins", "callback", "praetor_checkpoint.py"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(dockerfile) + string(execpack) + string(plugin)
	for _, required := range []string{"COPY praetor_checkpoint.py", "PRAETOR_DIAGNOSTIC_EVENTS", "HOST_", "task_failed", "host_unreachable"} {
		if !strings.Contains(contract, required) {
			t.Fatalf("execution pack diagnostics callback is missing %q", required)
		}
	}
}
