package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praetordev/praetor/internal/readiness"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"
const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func writeManifest(t *testing.T, journeys []readiness.JourneyEvidence) string {
	t.Helper()
	manifest := readiness.Manifest{SchemaVersion: 1, GeneratedAt: "2026-07-17T12:00:00Z", Revisions: readiness.Revisions{Praetor: testRevision, SecretsService: testRevision, Fixture: testRevision}, Journeys: journeys, Findings: []readiness.Finding{}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func allJourneys() []readiness.JourneyEvidence {
	return []readiness.JourneyEvidence{
		{Name: "ldap-operator", Result: "pass", EvidenceSHA256: testDigest},
		{Name: "secrets-service", Result: "pass", EvidenceSHA256: testDigest},
		{Name: "delegated-api", Result: "pass", EvidenceSHA256: testDigest},
		{Name: "execution-recovery", Result: "pass", EvidenceSHA256: testDigest},
	}
}

func TestRunWritesGoReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", writeManifest(t, allJourneys())}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "go"`) {
		t.Fatalf("unexpected report: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"findings": []`) {
		t.Fatalf("empty findings did not serialize as an array: %s", stdout.String())
	}
}

func TestRunWritesNoGoReportAndReturnsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	journeys := allJourneys()[:3]
	code := run([]string{"-input", writeManifest(t, journeys)}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"missing-journey:execution-recovery"`) {
		t.Fatalf("unexpected report: %s", stdout.String())
	}
}

func TestRunRejectsUnknownSecretField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsafe.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"generated_at":"2026-07-17T12:00:00Z","revisions":{},"journeys":[],"findings":[],"private_key":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-input", path}, &stdout, &stderr); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr.String(), "unknown field") || stdout.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
