package tests

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/ingestion/core"
	"github.com/praetordev/praetor/services/ingestion/handler"
)

func TestIngestionHandler(t *testing.T) {
	// Mock Service (or use nil DB if we just verify Handler routing/validation logic)
	// Since IngestEvents calls DB, passing nil DB will panic or fail if called.
	// We can't easily mock DB here without sqlmock.
	// However, we can test that INVALID requests are rejected before hitting DB.

	svc := core.NewIngestionService(nil, nil, nil)
	h := handler.NewIngestionHandler(svc)
	if h == nil {
		t.Fatal("Handler is nil")
	}

	// Construct a valid payload to verify model marshalling
	stdout := "Task completed"
	payload := []models.JobEvent{
		{
			EventType:     "runner_on_ok",
			StdoutSnippet: &stdout,
			CreatedAt:     time.Now(),
		},
	}
	body, _ := json.Marshal(payload)

	// Just check if we can form the request object correctly
	_ = httptest.NewRequest("POST", "/api/v1/runs/"+uuid.NewString()+"/events", bytes.NewReader(body))
}
