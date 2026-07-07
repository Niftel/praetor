package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// fakeLogIngestion captures uploaded chunks (by seq) and can simulate an outage.
type fakeLogIngestion struct {
	mu     sync.Mutex
	chunks map[int64][]byte
	fail   bool
	server *httptest.Server
}

func newFakeLogIngestion() *fakeLogIngestion {
	f := &fakeLogIngestion{chunks: map[int64][]byte{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		seq, _ := strconv.ParseInt(r.URL.Query().Get("seq"), 10, 64)
		body, _ := io.ReadAll(r.Body)
		f.chunks[seq] = body
		w.WriteHeader(http.StatusAccepted)
	}))
	return f
}

func (f *fakeLogIngestion) setFail(v bool) { f.mu.Lock(); f.fail = v; f.mu.Unlock() }

// reassemble concatenates chunks in seq order — the bytes the control plane has.
func (f *fakeLogIngestion) reassemble() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []byte
	for seq := int64(0); ; seq++ {
		c, ok := f.chunks[seq]
		if !ok {
			break
		}
		out = append(out, c...)
	}
	return out
}

func (f *fakeLogIngestion) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.chunks) }

// TestLogSyncerStreamsChunksAndResumes asserts the live-streaming guarantees:
// an outage delivers nothing and parks the cursor; recovery delivers the full
// stdout in order; and a restarted syncer resumes from the cursor, uploading
// only new output. The reassembled chunks always equal the on-disk stdout.
func TestLogSyncerStreamsChunksAndResumes(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "stdout.log")

	first := []byte("PLAY [all] ***\nTASK [ping] ***\nok: [web-01]\n")
	if err := os.WriteFile(logPath, first, 0644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	fake := newFakeLogIngestion()
	defer fake.server.Close()

	// Outage: nothing delivered, no cursor.
	fake.setFail(true)
	s := NewLogSyncer(dir, fake.server.URL, "run-1", "")
	s.offset, s.chunkSeq = s.readCursor()
	s.flush()
	if fake.count() != 0 {
		t.Fatalf("expected 0 chunks during outage, got %d", fake.count())
	}
	if _, err := os.Stat(filepath.Join(dir, "stdout.cursor")); err == nil {
		t.Fatalf("cursor must not advance while uploads fail")
	}

	// Recovery: the full first write is delivered.
	fake.setFail(false)
	s.flush()
	if got := fake.reassemble(); string(got) != string(first) {
		t.Fatalf("after recovery, control plane has %q, want %q", got, first)
	}

	// Append more output, then simulate a restart with a fresh syncer that only
	// knows the on-disk cursor: it must upload ONLY the appended bytes.
	second := []byte("TASK [install] ***\nchanged: [web-01]\nPLAY RECAP ***\n")
	fAppend, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	_, _ = fAppend.Write(second)
	fAppend.Close()

	s2 := NewLogSyncer(dir, fake.server.URL, "run-1", "")
	s2.offset, s2.chunkSeq = s2.readCursor()
	if s2.offset == 0 {
		t.Fatalf("restarted log syncer did not load a persisted cursor")
	}
	s2.flush()

	if got := fake.reassemble(); string(got) != string(first)+string(second) {
		t.Fatalf("final reassembled log mismatch:\n got %q\nwant %q", got, string(first)+string(second))
	}
}

// TestLogSyncerChunkSizeCap verifies output larger than maxLogChunk is split
// across multiple chunks that still reassemble to the original.
func TestLogSyncerChunkSizeCap(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "stdout.log")

	big := make([]byte, maxLogChunk*2+1024)
	for i := range big {
		big[i] = byte('a' + i%26)
	}
	if err := os.WriteFile(logPath, big, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fake := newFakeLogIngestion()
	defer fake.server.Close()

	s := NewLogSyncer(dir, fake.server.URL, "run-2", "")
	s.flush()

	if fake.count() < 3 {
		t.Fatalf("expected output split into >=3 chunks, got %d", fake.count())
	}
	if got := fake.reassemble(); string(got) != string(big) {
		t.Fatalf("chunked upload did not reassemble to original (%d vs %d bytes)", len(got), len(big))
	}
}
