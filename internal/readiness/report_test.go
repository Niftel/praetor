package readiness

import (
	"strings"
	"testing"
)

const revision = "0123456789abcdef0123456789abcdef01234567"
const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func validManifest() Manifest {
	journeys := make([]JourneyEvidence, 0, len(requiredJourneys))
	for _, name := range requiredJourneys {
		journeys = append(journeys, JourneyEvidence{Name: name, Result: "pass", EvidenceSHA256: digest})
	}
	return Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   "2026-07-17T12:00:00Z",
		Revisions:     Revisions{Praetor: revision, SecretsService: revision, Fixture: revision},
		Journeys:      journeys,
		Findings:      []Finding{},
	}
}

func TestGenerateGoReport(t *testing.T) {
	report, err := Generate(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision.Status != "go" || len(report.Decision.Reasons) != 0 {
		t.Fatalf("unexpected decision: %+v", report.Decision)
	}
}

func TestGenerateNoGoForFailedAndMissingEvidence(t *testing.T) {
	manifest := validManifest()
	manifest.Journeys[0].Result = "fail"
	manifest.Journeys[1].EvidenceSHA256 = ""
	manifest.Journeys = manifest.Journeys[:3]
	report, err := Generate(manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"journey-failed:ldap-operator", "missing-evidence:secrets-service", "missing-journey:execution-recovery"}
	if strings.Join(report.Decision.Reasons, ",") != strings.Join(want, ",") {
		t.Fatalf("reasons = %v, want %v", report.Decision.Reasons, want)
	}
	if report.Decision.Status != "no-go" {
		t.Fatalf("status = %q", report.Decision.Status)
	}
}

func TestGenerateNoGoForOpenReleaseBlocker(t *testing.T) {
	manifest := validManifest()
	manifest.Findings = []Finding{{ID: "SEC-1", Category: "security", Classification: "release-blocking", Status: "open", Issue: "https://github.com/Niftel/praetor/issues/999"}}
	report, err := Generate(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.Decision.Reasons, ",") != "open-release-blocker:SEC-1" {
		t.Fatalf("unexpected reasons: %v", report.Decision.Reasons)
	}
}

func TestSecurityFindingDefaultsToBlocking(t *testing.T) {
	manifest := validManifest()
	manifest.Findings = []Finding{{ID: "SEC-2", Category: "security", Classification: "non-blocking", Status: "open", Issue: "https://github.com/Niftel/praetor/issues/999"}}
	if _, err := Generate(manifest); err == nil || !strings.Contains(err.Error(), "unless an exception was reviewed") {
		t.Fatalf("unexpected error: %v", err)
	}
	manifest.Findings[0].SecurityExceptionReviewed = true
	if _, err := Generate(manifest); err != nil {
		t.Fatalf("reviewed exception rejected: %v", err)
	}
}

func TestNotApplicableRequiresAllowlistedJustificationCode(t *testing.T) {
	manifest := validManifest()
	manifest.Journeys[0] = JourneyEvidence{Name: "ldap-operator", Result: "not-applicable", JustificationCode: "customer bind password was secret"}
	if _, err := Generate(manifest); err == nil || !strings.Contains(err.Error(), "allowed not-applicable justification_code") {
		t.Fatalf("unexpected error: %v", err)
	}
	manifest.Journeys[0].JustificationCode = "platform-not-applicable"
	if _, err := Generate(manifest); err != nil {
		t.Fatalf("allowed justification rejected: %v", err)
	}
}

func TestDecodeRejectsUnallowlistedSecretBearingFields(t *testing.T) {
	raw := `{"schema_version":1,"generated_at":"2026-07-17T12:00:00Z","revisions":{},"journeys":[],"findings":[],"token":"do-not-copy"}`
	if _, err := Decode(strings.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFindingRequiresScopedIssue(t *testing.T) {
	manifest := validManifest()
	manifest.Findings = []Finding{{ID: "REL-1", Category: "reliability", Classification: "non-blocking", Status: "open", Issue: "https://example.test/ticket/1"}}
	if _, err := Generate(manifest); err == nil || !strings.Contains(err.Error(), "scoped Niftel/praetor issue") {
		t.Fatalf("unexpected error: %v", err)
	}
}
