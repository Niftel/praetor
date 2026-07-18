package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPilotAccessKeepsPrivateKeyOnSecretsWritePath(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-pilot-access.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"--rawfile key \"$PRIVATE_KEY\"", "--data-binary \"@$SECRET_REQUEST\"", "rm -f \"$SECRET_REQUEST\"",
		"'$encrypted$'", "secrets_service_id::text", "secrets_service_version", "credential_created",
		"Credential Use", "Inventory Use", "service principals receive no use assignment", "frontend-team", "audit_count",
		"auditor_token", "auditor API response was not sealed",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("pilot access contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{"echo $PRIVATE_KEY", "cat $PRIVATE_KEY", "inputs:{ssh_private_key:", "docker cp $PRIVATE_KEY"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("pilot access contract contains unsafe private-key handling %q", forbidden)
		}
	}
}

func TestPilotAccessSeedIsIdempotent(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-pilot-access.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{"ensure inventories", "find_id credentials", "grant_team_role", "length')\" == 1"} {
		if !strings.Contains(script, required) {
			t.Fatalf("pilot idempotency contract is missing %q", required)
		}
	}
}
