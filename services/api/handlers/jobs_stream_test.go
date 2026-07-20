package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiagnosticStreamCursor(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		header  string
		want    int64
		wantErr bool
	}{
		{name: "default", want: 0},
		{name: "query", query: "?cursor=19", want: 19},
		{name: "resume header wins", query: "?cursor=19", header: "23", want: 23},
		{name: "negative", query: "?cursor=-1", wantErr: true},
		{name: "not a number", header: "event-4", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/stream"+test.query, nil)
			req.Header.Set("Last-Event-ID", test.header)
			got, err := diagnosticStreamCursor(req)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("cursor=%d err=%v, want cursor=%d err=%v", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestDiagnosticTerminal(t *testing.T) {
	for _, state := range []string{"successful", "failed", "canceled", "error", "lost"} {
		if !diagnosticTerminal(state) {
			t.Errorf("state %q should close the stream", state)
		}
	}
	for _, state := range []string{"pending", "running", "waiting"} {
		if diagnosticTerminal(state) {
			t.Errorf("state %q should keep the stream open", state)
		}
	}
}
