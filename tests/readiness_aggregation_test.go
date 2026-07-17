package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const readinessRevision = "0123456789abcdef0123456789abcdef01234567"

func writeJourneyEvidence(t *testing.T, dir, name string, extra map[string]any) {
	t.Helper()
	value := map[string]any{"schema_version": 1, "journey": name, "result": "pass"}
	for key, item := range extra {
		value[key] = item
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runReadinessAggregator(t *testing.T, evidence string) ([]byte, error) {
	t.Helper()
	root := repositoryRoot(t)
	output := filepath.Join(t.TempDir(), "report.json")
	cmd := exec.Command(filepath.Join(root, "scripts", "generate-readiness-report.sh"))
	cmd.Env = append(os.Environ(),
		"GOCACHE=/tmp/praetor-go-cache",
		"PRAETOR_READINESS_EVIDENCE_DIR="+evidence,
		"PRAETOR_READINESS_REPORT="+output,
		"PRAETOR_READINESS_GENERATED_AT=2026-07-17T12:00:00Z",
		"PRAETOR_REVISION="+readinessRevision,
		"PRAETOR_SECRETS_REVISION="+readinessRevision,
	)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		return combined, err
	}
	report, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return report, nil
}

func TestReadinessAggregatorProducesSanitizedGoReport(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ldap-operator", "secrets-service", "delegated-api", "execution-recovery"} {
		writeJourneyEvidence(t, dir, name, map[string]any{"run_id": "synthetic-id"})
	}
	report, err := runReadinessAggregator(t, dir)
	if err != nil {
		t.Fatalf("aggregate: %v: %s", err, report)
	}
	if !strings.Contains(string(report), `"status": "go"`) || strings.Contains(string(report), "synthetic-id") {
		t.Fatalf("report was not sanitized: %s", report)
	}
}

func TestReadinessAggregatorRejectsMissingAndSensitiveEvidence(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ldap-operator", "secrets-service", "delegated-api"} {
		writeJourneyEvidence(t, dir, name, nil)
	}
	output, err := runReadinessAggregator(t, dir)
	if err == nil || !strings.Contains(string(output), "missing evidence artifact") {
		t.Fatalf("unexpected result: err=%v output=%s", err, output)
	}

	writeJourneyEvidence(t, dir, "execution-recovery", map[string]any{"private_key": "do-not-copy"})
	output, err = runReadinessAggregator(t, dir)
	if err == nil || !strings.Contains(string(output), "sensitive field name detected") {
		t.Fatalf("unexpected result: err=%v output=%s", err, output)
	}
}
