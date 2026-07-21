package tests

import (
	"os"
	"strings"
	"testing"
)

func TestInventorySyncHistoryMigrationContract(t *testing.T) {
	data, err := os.ReadFile("../db/migrations/000070_inventory_sync_history.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS inventory_sync_history",
		"correlation_id",
		"reconciliation_policy",
		"disable_missing",
		"retain_missing",
		"diagnostic_details",
		"CREATE TRIGGER trg_create_inventory_sync_history",
		"inventory_preview",
		"ON DELETE SET NULL",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("sync history migration is missing %q", required)
		}
	}
}
