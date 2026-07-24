package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFleetScaleValidationIsReleaseGradeAndSanitized(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-fleet-scale-e2e.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"TEST_DATABASE_URL",
		"TestBulk",
		"TestDelegatedWorkflowLaunch",
		"services/api.bulk.test.ts",
		"components/ui/BulkSelection.test.tsx",
		"pages/FleetScaleJourney.test.tsx",
		"pages/frameworkAdoption.test.ts",
		`journey:"fleet-scale"`,
		`"organization-boundary"`,
		`"inventory-rbac"`,
		`"delegated-client"`,
		`"request-bounds"`,
		`"payload-bounds"`,
		`"idempotent-replay"`,
		`"concurrent-replay"`,
		`"preview-confirm-delete"`,
		`"stale-preview"`,
		`"audit-attribution"`,
		`"partial-result-retry"`,
		`"browser-selection-execution-results-retry"`,
		"PRAETOR_FLEET_EVIDENCE_FILE",
		"umask 077",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("fleet validation must contain %q", required)
		}
	}
	for _, forbidden := range []string{`"private_key":`, `"password":`, `"bearer":`, `"token_value":`, `"database_url":`} {
		if strings.Contains(strings.ToLower(script), forbidden) {
			t.Fatalf("fleet evidence contract contains sensitive field %q", forbidden)
		}
	}
}

func TestFleetScaleLiveValidationUsesSyntheticBoundedResources(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-fleet-scale-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"bulk/hosts/create",
		"bulk/jobs/launch",
		"bulk/hosts/delete/preview",
		"bulk/hosts/delete",
		"activity-stream?limit=1000",
		"not_found_or_forbidden",
		"9223372036854775000",
		"range(0;26)",
		`journey:"fleet-scale-live"`,
		"PRAETOR_FLEET_LIVE_EVIDENCE_FILE",
		"PRAETOR_FLEET_BOOTSTRAP_FIXTURE",
		"umask 077",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("live fleet validation must contain %q", required)
		}
	}
	for _, forbidden := range []string{`"private_key":`, `"bearer_token":`, `"token_value":`, `"password":`, `"database_url":`} {
		if strings.Contains(strings.ToLower(script), forbidden) {
			t.Fatalf("live fleet evidence contract contains sensitive field %q", forbidden)
		}
	}
}
