package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/praetordev/models"
)

func TestModelsInstantiate(t *testing.T) {
	// Simple smoke test to ensure models package is imported correctly and structs exist
	now := time.Now()

	// Auth
	_ = models.Organization{ID: 1, Name: "Test Org", CreatedAt: now}

	// Resources
	_ = models.Project{ID: 1, Name: "Test Project", SCMType: "git"}

	// Execution
	uid := uuid.New()
	_ = models.ExecutionRun{
		ID:           uid,
		UnifiedJobID: 100,
		State:        "pending",
	}

	// JSON Marshalling check
	job := models.UnifiedJob{
		ID:      1,
		Name:    "Test Job",
		JobArgs: json.RawMessage(`{"foo": "bar"}`),
	}

	bytes, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Failed to marshal UnifiedJob: %v", err)
	}
	t.Logf("Marshalled UnifiedJob: %s", string(bytes))
}
