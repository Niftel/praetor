package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPostFactsMatchesV1Contract(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "tests", "contracts", "v1", "fact_cache_upload.json"))
	if err != nil {
		t.Fatal(err)
	}

	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/runs/run-1/facts" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer run-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	var body struct {
		Facts map[string]json.RawMessage `json:"facts"`
	}
	if err := json.Unmarshal(want, &body); err != nil {
		t.Fatal(err)
	}
	postFacts(server.URL, "run-1", "run-token", body.Facts)

	var wantJSON, gotJSON any
	if err := json.Unmarshal(want, &wantJSON); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &gotJSON); err != nil {
		t.Fatal(err)
	}
	wantCanonical, _ := json.Marshal(wantJSON)
	gotCanonical, _ := json.Marshal(gotJSON)
	if string(gotCanonical) != string(wantCanonical) {
		t.Fatalf("fact upload mismatch:\ngot  %s\nwant %s", gotCanonical, wantCanonical)
	}
}
