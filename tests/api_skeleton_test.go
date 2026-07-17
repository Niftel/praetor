package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/praetordev/praetor/services/api"
)

func TestAPISkeletonPing(t *testing.T) {
	// Pass nil DB for skeleton test (ping doesn't need DB)
	router := api.NewRouter(nil, api.Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var data map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if data["status"] != "pong" {
		t.Errorf("Expected pong, got %v", data["status"])
	}
}

func TestAPIReadinessRequiresDatabase(t *testing.T) {
	router := api.NewRouter(nil, api.Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness without database: want 503, got %d (%s)", rec.Code, rec.Body)
	}
}
