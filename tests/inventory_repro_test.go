package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/services/api"
)

func TestCreateInventoryRepro(t *testing.T) {
	// Connect to local DB
	dbURL := "postgres://postgres:postgres@localhost:5432/praetor?sslmode=disable"
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("Skipping integration test: failed to connect to DB: %v", err)
	}
	defer db.Close()

	// Use full router to test middleware + routing
	router := api.NewRouter(db, api.Config{})

	// Payload mimicking App.tsx
	payload := map[string]interface{}{
		"name":            fmt.Sprintf("test-inv-router-%d", 12345),
		"organization_id": 1,
	}
	body, _ := json.Marshal(payload)

	// Note: App.tsx sends to /api/v1/inventories
	req := httptest.NewRequest("POST", "/api/v1/inventories", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 201/200, got %d", resp.StatusCode)
		// Print body
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		t.Logf("Response Body: %s", buf.String())
	} else {
		t.Log("Successfully created inventory via Router")
	}

	// Cleanup
	_, _ = db.Exec("DELETE FROM inventories WHERE name = $1", payload["name"])
}
