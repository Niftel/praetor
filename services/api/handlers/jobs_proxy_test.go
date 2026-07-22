package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestNewJobsResourceValidatesIngestionOrigin(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"http:///missing-host",
		"http://user:password@ingestion:8081",
		"http://ingestion:8081/unexpected/path",
		"http://ingestion:8081?target=other",
		"http://ingestion:8081#fragment",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NewJobsResource(nil, raw, "", nil); err == nil {
				t.Fatalf("NewJobsResource(%q) accepted an unsafe ingestion origin", raw)
			}
		})
	}
}

func TestIngestionLogRequestUsesOnlyConfiguredOrigin(t *testing.T) {
	resource, err := NewJobsResource(nil, "https://ingestion.example:8443/", "internal-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	runID := uuid.MustParse("047da59d-8d0d-4e82-9333-e3f258d8d8be")
	req, err := resource.newIngestionLogRequest(context.Background(), runID, "-1")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := req.URL.String(), "https://ingestion.example:8443/api/v1/runs/047da59d-8d0d-4e82-9333-e3f258d8d8be/logs?since=-1"; got != want {
		t.Fatalf("log URL = %q, want %q", got, want)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer internal-token" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestIngestionClientDoesNotFollowRedirects(t *testing.T) {
	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	resource, err := NewJobsResource(nil, origin.URL, "internal-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	req, err := resource.newIngestionLogRequest(context.Background(), uuid.New(), "-1")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := resource.ingestionClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := redirectedRequests.Load(); got != 0 {
		t.Fatalf("redirect target received %d request(s), want 0", got)
	}
}
