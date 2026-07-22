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
		"resource_cleanup true", "credentials/$CREDENTIAL_ID",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("dynamic inventory journey is missing %q", required)
		}
	}
	for _, forbidden := range []string{"set -x", "echo $SECRET", "docker system prune", "k3d cluster delete"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("dynamic inventory journey contains unsafe operation %q", forbidden)
		}
	}
}

func TestProductValidationRunsDynamicInventoryJourney(t *testing.T) {
	root := repositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "product-validation-fixture.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"validate-dynamic-inventory-e2e.sh", "dynamic-inventory.json", "Upload bounded dynamic-inventory diagnostics", "if: always()"} {
		if !strings.Contains(string(workflow), required) {
			t.Errorf("product validation workflow is missing %q", required)
		}
	}
	if strings.Count(string(workflow), "./scripts/validate-dynamic-inventory-e2e.sh") != 2 {
		t.Error("product validation must prove the dynamic inventory journey is repeatable from cleanup")
	}
	fixture, err := os.ReadFile(filepath.Join(root, "deployments", "product-validation", "fixture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"praetor-validation-inventory-provider", "automountServiceAccountToken: false", "Cache-Control \"no-store\""} {
		if !strings.Contains(string(fixture), required) {
			t.Errorf("synthetic inventory provider is missing %q", required)
		}
	}
}
