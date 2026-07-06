package plog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

// TestNewEmitsThroughConfiguredDefault guards the double-wrap regression: a
// package-level logger from New() is created before the default handler is
// installed, but must still emit a single structured record through whatever
// handler is the default at log time — not capture slog's bootstrap default
// (which would render text and re-wrap it inside the real handler).
func TestNewEmitsThroughConfiguredDefault(t *testing.T) {
	// logger created BEFORE the default handler is installed (mimics a package var).
	logger := New("scheduler")

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))) })

	logger.Info("scheduler started", "job_id", 7)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("output is not a single JSON record: %v\ngot: %s", err, buf.String())
	}
	if rec["msg"] != "scheduler started" {
		t.Errorf("msg = %q, want %q (double-wrapped?)", rec["msg"], "scheduler started")
	}
	if rec["component"] != "scheduler" {
		t.Errorf("component = %v, want scheduler", rec["component"])
	}
	if rec["job_id"] != float64(7) {
		t.Errorf("job_id = %v, want 7", rec["job_id"])
	}
}
