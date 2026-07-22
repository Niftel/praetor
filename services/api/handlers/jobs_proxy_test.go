package handlers

import (
	"context"
	"io"
	"net/http"
	"strings"
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
	req, err := resource.ingestionLogs.newRequest(context.Background(), runID, "-1")
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
	transport := &redirectRoundTripper{location: "https://attacker.example/logs"}
	resource, err := NewJobsResource(nil, "https://ingestion.example:8443", "internal-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	resource.ingestionLogs.httpClient.Transport = transport
	resp, err := resource.ingestionLogs.fetch(context.Background(), uuid.New(), "-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := transport.calls.Load(); got != 1 {
		t.Fatalf("transport received %d request(s), want only the initial request", got)
	}
}

func TestIngestionClientRejectsChangedOriginBeforeTransport(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
	}{
		{name: "host", url: "https://attacker.example/api/v1/runs/047da59d-8d0d-4e82-9333-e3f258d8d8be/logs"},
		{name: "scheme", url: "http://ingestion.example:8443/api/v1/runs/047da59d-8d0d-4e82-9333-e3f258d8d8be/logs"},
		{name: "userinfo", url: "https://user@ingestion.example:8443/api/v1/runs/047da59d-8d0d-4e82-9333-e3f258d8d8be/logs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &countingRoundTripper{}
			client, err := newIngestionLogClient("https://ingestion.example:8443", "internal-token")
			if err != nil {
				t.Fatal(err)
			}
			client.httpClient.Transport = transport
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := client.do(req); err == nil || !strings.Contains(err.Error(), "does not match configured origin") {
				t.Fatalf("do() error = %v, want origin mismatch", err)
			}
			if got := transport.calls.Load(); got != 0 {
				t.Fatalf("transport received %d request(s), want 0", got)
			}
		})
	}
}

type countingRoundTripper struct {
	calls atomic.Int32
}

func (t *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}, nil
}

type redirectRoundTripper struct {
	calls    atomic.Int32
	location string
}

func (t *redirectRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusFound,
		Body:       io.NopCloser(strings.NewReader("redirect")),
		Header:     http.Header{"Location": []string{t.location}},
	}, nil
}
