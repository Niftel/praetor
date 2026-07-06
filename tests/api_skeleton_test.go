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
	ts := httptest.NewServer(router)
	defer ts.Close()

	// Test Ping
	resp, err := http.Get(ts.URL + "/api/v1/ping")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var data map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if data["status"] != "pong" {
		t.Errorf("Expected pong, got %v", data["status"])
	}
}
