package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDynamicInventoryStagingJourneyContract(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-dynamic-inventory-e2e.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"PRAETOR_DYNAMIC_INVENTORY_EVIDENCE_FILE", "ANSIBLE_PASSWORD_FILE", "built-in Machine credential type",
		"source_type:\"custom\"", "source_kind:\"script\"", "reconciliation_policy:\"disable_missing\"",
		"/preview", "/sync", "/history", "Inventory Update", "Credential Use",
		"hosts_added == 2", "hosts_updated == 1", "hosts_disabled == 1", "hosts_unchanged == 2",
		"diagnostic_details == {}", "unauthorized team", "credential leaked into history",
		"invalid-credential", "provider_acquisition_failed", "provider-timeout", "provider_timeout", "time.sleep(70)", "record_failure", "phase:$phase",
		"inventory_source_id:$source", "dynamic-inventory", "secret-redaction", "PHASE=\"cleanup\"",
		"resource_cleanup true", "credentials/$CREDENTIAL_ID", "set -Eeuo pipefail",
		"GET /api/v1/$path returned $status", "failed during phase '$PHASE'", "PHASE=\"resource-discovery\"",
		`get "$ADMIN_TOKEN" jobs`, `.[] | select(.id == $id) | .status`,
		"PRAETOR_DYNAMIC_INVENTORY_DB_OBSERVER_POD", "SELECT status FROM unified_jobs WHERE id=$job_id",
		`.results[0].status | IN("successful", "failed", "error", "canceled")`, "terminal entries",
		"kubectl rollout restart", "deployment/praetor-validation-inventory-provider", "--timeout=60s",
		`"$STATUS" == 204`,
		"notification-policies", "resource_type:\"inventory_source\"", "wait_notification",
		"notification-exact-once", "notification-resource-identity", "notification-secret-redaction",
		"praetor-validation-notification-sink", `.kind == "inventory sync"`,
	} {
		if !strings.Contains(script, required) {
			t.Errorf("dynamic inventory journey is missing %q", required)
		}
	}
	for _, nonCanonical := range []string{
		"find_id organizations/", "find_id teams/", "find_id credential-types/",
		`"inventories/$INVENTORY_ID/hosts/"`, "POST schedules/", "schedules/ |",
	} {
		if strings.Contains(script, nonCanonical) {
			t.Errorf("dynamic inventory journey uses non-canonical collection path %q", nonCanonical)
		}
	}
	for _, forbidden := range []string{"set -x", "echo $SECRET", "docker system prune", "k3d cluster delete"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("dynamic inventory journey contains unsafe operation %q", forbidden)
		}
	}
	if strings.Contains(script, `"jobs/$job_id"`) {
		t.Error("dynamic inventory journey must not poll the unsupported GET /jobs/{id} route")
	}
}

func TestProductValidationRunsDynamicInventoryJourney(t *testing.T) {
	root := repositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "product-validation-fixture.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"validate-dynamic-inventory-e2e.sh", "dynamic-inventory.json", "Upload bounded journey diagnostics", "if: always()", "needs.preflight.outputs.run_dynamic == 'true'"} {
		if !strings.Contains(string(workflow), required) {
			t.Errorf("product validation workflow is missing %q", required)
		}
	}
	if strings.Count(string(workflow), "./scripts/validate-dynamic-inventory-e2e.sh") != 1 {
		t.Error("product validation must run the expensive dynamic inventory journey exactly once")
	}
	fixture, err := os.ReadFile(filepath.Join(root, "deployments", "product-validation", "fixture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"praetor-validation-inventory-provider", "automountServiceAccountToken: false", "Cache-Control \"no-store\"", "limits: {cpu: 50m, memory: 64Mi}"} {
		if !strings.Contains(string(fixture), required) {
			t.Errorf("synthetic inventory provider is missing %q", required)
		}
	}
}
