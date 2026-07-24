package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseCompatibilityMatrixIsRequiredByCI(t *testing.T) {
	root := repositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "scripts", "database-compatibility.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"database-compatibility:",
		"./scripts/database-compatibility.sh",
		"postgres:15",
	} {
		if !strings.Contains(string(workflow), required) {
			t.Fatalf("CI migration compatibility job is missing %q", required)
		}
	}
	for _, required := range []string{
		"55 62 65 67",
		"migrationfixture rollback 77",
		"go run ./cmd/migrator",
	} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("migration compatibility matrix is missing %q", required)
		}
	}
}
