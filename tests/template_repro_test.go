package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/api/handlers"
)

func TestListTemplatesRepro(t *testing.T) {
	// Connect to local DB
	dbURL := "postgres://postgres:postgres@localhost:5432/praetor?sslmode=disable"
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("Skipping integration test: failed to connect to DB: %v", err)
	}
	defer db.Close()

	// Ensure at least one template exists
	var ujID int64
	// Try creating
	err = db.QueryRow(`INSERT INTO unified_job_templates (organization_id, name, unified_job_type) VALUES (1, 'repro-template', 'job') ON CONFLICT (organization_id, name) DO NOTHING RETURNING id`).Scan(&ujID)
	// If ID is 0, fetch it
	if ujID == 0 {
		err = db.QueryRow(`SELECT id FROM unified_job_templates WHERE name = 'repro-template'`).Scan(&ujID)
		if err != nil {
			t.Fatalf("Failed to fetch existing UJT: %v", err)
		}
	}

	_, err = db.Exec(`
		INSERT INTO job_templates (organization_id, name, playbook, job_type, unified_job_template_id) 
		VALUES (1, 'repro-template', 'site.yml', 'run', $1)
		ON CONFLICT (organization_id, name) DO NOTHING
	`, ujID)
	if err != nil {
		t.Fatalf("Failed to seed template: %v", err)
	}

	resource := handlers.NewTemplatesResource(db)
	req := httptest.NewRequest("GET", "/api/v1/job-templates", nil)
	w := httptest.NewRecorder()

	resource.ListTemplates(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify we can unmarshal the response
	var result struct {
		Items []models.JobTemplate `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if len(result.Items) == 0 {
		t.Log("Warning: No templates found (might be valid if DB was empty before seed?)")
	} else {
		t.Logf("Found %d templates", len(result.Items))
	}

	// Cleanup
	_, _ = db.Exec("DELETE FROM job_templates WHERE name = 'repro-template'")
	_, _ = db.Exec("DELETE FROM unified_job_templates WHERE name = 'repro-template'")
}
