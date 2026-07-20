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
	if err != nil { t.Fatal(err) }
	contract := string(raw) + readMakefile(t, root)
	for _, required := range []string{
		"staging-execution-diagnostics-preflight", "staging-execution-diagnostics-verify", "failed_task", "rejected_approval", "runner_bootstrap_failure",
		"control-plane interruption", "relaunch", "duplicates == 0", "gaps == 0", "cross_team_denied",
		"auditor_mutations_denied", "event_count >= 100", "api_p95_ms <= 750", "render_p95_ms <= 1500",
		"mobile_390x844", "PRAETOR_SECRET_CANARY", "sha256:[0-9a-f]{64}", "chmod 0600", "deployed API does not expose execution diagnostics",
	} {
		if !strings.Contains(contract, required) { t.Fatalf("staging diagnostics gate is missing %q", required) }
	}
}
