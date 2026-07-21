package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStagingServicePrincipalFixtureSafetyContract(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "scripts", "staging-service-principal-fixture.sh")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	required := []string{
		"set -euo pipefail",
		`CONTEXT="${PRAETOR_STAGING_CONTEXT:-k3d-praetor-staging}"`,
		`kube() { kubectl --context "$CONTEXT" "$@"; }`,
		"PRAETOR_DELEGATED_FIXTURE_FAIL_AFTER_CREDENTIAL",
		"multiple fixture principals exist; refusing an ambiguous cleanup",
		"refusing to delete unlabelled Secret",
		"allowed_host_ids:[$host]",
		"max_hosts:$max",
		"allowed_extra_var_keys:[$extra]",
		"approval_team_id:$team",
		".token | @base64",
		"unset response",
		".replayed == true",
		"active credentials, expected 1",
		"active grants, expected 1",
		"service-principals/$principal/credentials/$active_id/rotate",
		"service-principals/$principal/grants/$grant_id",
	}
	for _, contract := range required {
		if !strings.Contains(script, contract) {
			t.Errorf("delegated staging fixture is missing safety contract %q", contract)
		}
	}
	for _, forbidden := range []string{"set -x", "echo $service_token", "echo $response", "--from-literal=token"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("delegated staging fixture contains secret-leaking pattern %q", forbidden)
		}
	}
}

func TestStagingServicePrincipalFixtureDocumentsFullRehearsal(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "PRODUCT_VALIDATION_FIXTURE.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, contract := range []string{
		"make delegated-fixture-plan",
		"make delegated-fixture-setup",
		"make delegated-fixture-validate",
		"make delegated-fixture-cleanup",
		"make delegated-fixture-rehearse",
		"without duplicate active credentials or grants",
	} {
		if !strings.Contains(doc, contract) {
			t.Errorf("delegated staging documentation is missing %q", contract)
		}
	}
}
