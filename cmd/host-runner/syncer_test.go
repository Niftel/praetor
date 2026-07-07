package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/praetordev/praetor/pkg/events"
)

// fakeIngestion records received event sequences and can be toggled to fail,
// simulating a control-plane outage.
type fakeIngestion struct {
	mu       sync.Mutex
	received []int64
	fail     bool
	server   *httptest.Server
}

func newFakeIngestion() *fakeIngestion {
	f := &fakeIngestion{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		var batch []events.JobEvent
		_ = json.NewDecoder(r.Body).Decode(&batch)
		for _, e := range batch {
			f.received = append(f.received, e.Seq)
		}
		w.WriteHeader(http.StatusOK)
	}))
	return f
}

func (f *fakeIngestion) setFail(v bool) { f.mu.Lock(); f.fail = v; f.mu.Unlock() }
func (f *fakeIngestion) seqs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int64, len(f.received))
	copy(out, f.received)
	return out
}

func writeWAL(t *testing.T, path string, seqs ...int64) {
	t.Helper()
	w := NewWAL(path)
	defer w.Close()
	runID := uuid.New()
	for _, s := range seqs {
		if err := w.Append(&events.JobEvent{ExecutionRunID: runID, Seq: s, EventType: "JOB_STDOUT"}); err != nil {
			t.Fatalf("wal append: %v", err)
		}
	}
}

// TestSyncerCursorSurvivesOutageAndRestart asserts the two P1 guarantees:
//  1. a failed push does not drop or skip the event (the cursor stays parked);
//  2. a fresh Syncer resumes from the persisted cursor instead of replaying.
func TestSyncerCursorSurvivesOutageAndRestart(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "events.jsonl")
	writeWAL(t, walPath, 1, 2, 3)

	fake := newFakeIngestion()
	defer fake.server.Close()

	// Outage: every push fails, so nothing must be acknowledged or advanced.
	fake.setFail(true)
	s := NewSyncer(dir, fake.server.URL, "run-1", "")
	s.offset = s.readCursor()
	s.flush()

	if got := len(fake.seqs()); got != 0 {
		t.Fatalf("expected 0 events received during outage, got %d", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "events.cursor")); err == nil {
		t.Fatalf("cursor must not advance while pushes fail")
	}

	// Recovery: pushes succeed; all three events arrive exactly once, in order.
	fake.setFail(false)
	s.flush()

	assertSeqs(t, fake.seqs(), []int64{1, 2, 3})

	// Append more, then simulate a process restart with a brand-new Syncer that
	// only knows the on-disk cursor. It must deliver ONLY the new events.
	writeWAL(t, walPath, 4, 5)
	s2 := NewSyncer(dir, fake.server.URL, "run-1", "")
	s2.offset = s2.readCursor()
	if s2.offset == 0 {
		t.Fatalf("restarted syncer did not load a persisted cursor")
	}
	s2.flush()

	assertSeqs(t, fake.seqs(), []int64{1, 2, 3, 4, 5})
}

func assertSeqs(t *testing.T, got, want []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("seq mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("seq mismatch at %d: got %v want %v", i, got, want)
		}
	}
}
