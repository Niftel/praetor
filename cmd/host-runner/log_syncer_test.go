package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		// Resume-point endpoint: total stored bytes + max seq (-1 if none).
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/logs/cursor") {
			var bytes int64
			maxSeq := int64(-1)
			for seq, c := range f.chunks {
				bytes += int64(len(c))
				if seq > maxSeq {
					maxSeq = seq
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]int64{"bytes": bytes, "seq": maxSeq})
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

// TestLogSyncerRecoversFromCursorLoss is the #9 regression: if the local cursor
// vanishes mid-run while stdout.log still holds output, the syncer must recover
// its position from the server and append only the new bytes — NOT re-read from
// offset 0 (which would overwrite stored chunks and strand others, corrupting the
// reassembled log). Before the fix, loadCursor gave (0,0) and flush re-chunked
// from 0, producing duplicated/garbled output.
func TestLogSyncerRecoversFromCursorLoss(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "stdout.log")

	fake := newFakeLogIngestion()
	defer fake.server.Close()

	// Simulate progress already stored before the cursor was lost: two chunks the
	// server has, plus more bytes now on disk that were never pushed.
	fake.mu.Lock()
	fake.chunks[0] = []byte("PLAY [all] ***\n")  // seq 0
	fake.chunks[1] = []byte("TASK [ping] ***\n") // seq 1
	fake.mu.Unlock()

	appended := []byte("ok: [web-01]\nPLAY RECAP ***\n")
	full := append(append([]byte{}, []byte("PLAY [all] ***\nTASK [ping] ***\n")...), appended...)
	if err := os.WriteFile(logPath, full, 0644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	// No cursor file on disk -> loadCursor must flag a resync (not start at 0).
	s := NewLogSyncer(dir, fake.server.URL, "run-x", "")
	s.loadCursor()
	if !s.needResync {
		t.Fatal("expected needResync after losing the cursor with data on disk")
	}

	s.flush() // recovers offset/seq from the server, then appends only new bytes

	// The full stdout must reassemble exactly — no duplication, no gap.
	if got := fake.reassemble(); string(got) != string(full) {
		t.Fatalf("after cursor-loss recovery, reassembled:\n got %q\nwant %q", got, full)
	}
	// Existing chunks must be untouched (write-once), and the new bytes land in a
	// fresh seq starting exactly where the server left off.
	if string(fake.chunks[0]) != "PLAY [all] ***\n" || string(fake.chunks[1]) != "TASK [ping] ***\n" {
		t.Fatal("existing stored chunks must not be overwritten on recovery")
	}
	if string(fake.chunks[2]) != string(appended) {
		t.Fatalf("appended bytes should be a new chunk seq2=%q, got %q", appended, fake.chunks[2])
	}
	if s.offset != int64(len(full)) {
		t.Fatalf("offset after recovery = %d, want %d", s.offset, len(full))
	}
}
