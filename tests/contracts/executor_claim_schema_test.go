package contracts

import (
	"os"
	"strings"
	"testing"
)

func TestExecutorClaimMigrationContainsSecurityFences(t *testing.T) {
	content, err := os.ReadFile("../../db/migrations/000064_executor_credential_claim.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, required := range []string{
		"dispatch_id UUID", "secrets_credential_id UUID", "secrets_credential_version BIGINT",
		"executor_identity TEXT", "credential_binding_created_at TIMESTAMPTZ",
		"execution_runs_secrets_credential_pair", "execution_runs_executor_claim_pair",
		"CREATE UNIQUE INDEX", "prevent_execution_run_claim_reassignment", "BEFORE UPDATE",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("claim migration is missing %q", required)
		}
	}
}
