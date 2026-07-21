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

func TestExecutionDiagnosticsBudgetFixtureIsBoundedAndDisposable(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "generate-staging-diagnostic-budgets.sh"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(raw) + readMakefile(t, root)
	for _, required := range []string{
		"staging-execution-diagnostics-measure", "generate_series(1,125)", "generate_series(126,251)",
		"--max-time 2", "Last-Event-ID", "disconnect_cursor:125", "terminal_cursor:251",
		"TOTAL_IDS - UNIQUE_IDS", "api_p95_ms", "render_p95_ms", "limit=100",
		"demo-auditor", "fwalsh", "AUDITOR_MUTATION", "CROSS_TEAM_READ", "DELETE FROM unified_jobs",
		"Failed task fixture", "Runner bootstrap fixture", "Rejected approval fixture", "was not projected",
		"temporary-fixture-cleanup", "PRAETOR_SECRET_CANARY", "chmod 0600",
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("diagnostics budget fixture is missing %q", required)
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
