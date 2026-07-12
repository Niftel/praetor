package events_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/praetordev/praetor/pkg/events"
)

func TestExecutionRequestSerialization(t *testing.T) {
	uid := uuid.New()
	req := events.ExecutionRequest{
		ExecutionRunID: uid,
		UnifiedJobID:   123,
		JobManifest: events.JobManifest{
			Inventory:  "hosts",
			ProjectURL: "https://github.com/example/repo.git",
			Playbook:   "playbook.yml",
			ExtraVars:  map[string]interface{}{"foo": "bar"},
		},
		CreatedAt: time.Now(),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	var decoded events.ExecutionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	if decoded.ExecutionRunID != uid {
		t.Errorf("Expected UUID %s, got %s", uid, decoded.ExecutionRunID)
	}
	if decoded.UnifiedJobID != 123 {
		t.Errorf("Expected JobID 123, got %d", decoded.UnifiedJobID)
	}
	if decoded.JobManifest.ExtraVars["foo"] != "bar" {
		t.Errorf("Expected extra var foo=bar, got %v", decoded.JobManifest.ExtraVars["foo"])
	}
}
