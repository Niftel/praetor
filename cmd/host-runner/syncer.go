package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/praetordev/praetor/pkg/events"
)

// Syncer ships the host-local WAL back to the control plane. It tracks a
// persistent byte cursor that is advanced only after the control plane confirms
// receipt, so a failed push or a host-runner restart re-delivers events from the
// last acknowledged position rather than dropping them or replaying the whole
// log. Combined with the consumer's idempotency, this yields exactly-once effect.
type Syncer struct {
	JobDir string
	APIURL string
	RunID  string
	Token  string
	Client *http.Client

	walPath    string
	cursorPath string
	offset     int64
}

func NewSyncer(jobDir, apiURL, runID, token string) *Syncer {
	return &Syncer{
		JobDir:     jobDir,
		APIURL:     apiURL,
		RunID:      runID,
		Token:      token,
		Client:     &http.Client{Timeout: 5 * time.Second},
		walPath:    filepath.Join(jobDir, "events.jsonl"),
		cursorPath: filepath.Join(jobDir, "events.cursor"),
	}
}

func (s *Syncer) Start(done chan bool) {
	log.Printf("Starting Syncer for Run %s to %s", s.RunID, s.APIURL)
	s.offset = s.readCursor()
	if s.offset > 0 {
		log.Printf("Syncer: resuming from cursor offset %d", s.offset)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			log.Println("Syncer stopping, final flush...")
			s.flush()
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

// maxEventBatch bounds a single events POST so a burst of output ships as a few
// batched requests rather than one POST per event (#47), while keeping the request
// well under the transport/JetStream size limits.
const (
	maxEventBatch      = 500
	maxEventBatchBytes = 512 * 1024
)

// flush pushes every complete, not-yet-synced line from the WAL, batched into a
// few POSTs rather than one per event. The cursor is advanced (in memory,
// persisted once at the end) only past events the control plane confirmed
// receiving, so a push failure leaves the cursor parked and the batch is retried
// on the next tick (the consumer dedups any re-sent event on (run, seq)).
func (s *Syncer) flush() {
	f, err := os.Open(s.walPath)
	if err != nil {
		return // WAL not created yet
	}
	defer f.Close()

	if _, err := f.Seek(s.offset, io.SeekStart); err != nil {
		log.Printf("Syncer: seek to %d failed: %v", s.offset, err)
		return
	}

	reader := bufio.NewReader(f)
	advanced := false
	batch := make([]events.JobEvent, 0, 64)
	var batchBytes int64 // WAL bytes represented by the pending batch

	// sendBatch POSTs the accumulated events and, on success, advances the cursor
	// past exactly those WAL bytes. Returns false on failure so the caller stops
	// (cursor parked at the batch start; retried next tick).
	sendBatch := func() bool {
		if len(batch) == 0 {
			return true
		}
		if err := s.push(batch); err != nil {
			log.Printf("Syncer: batch push of %d event(s) failed at offset %d (will retry): %v", len(batch), s.offset, err)
			return false
		}
		s.offset += batchBytes
		advanced = true
		batch = batch[:0]
		batchBytes = 0
		return true
	}

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// No newline-terminated record: EOF or a partial trailing write. Never
			// advance past an incomplete record.
			break
		}

		var evt events.JobEvent
		if jsonErr := json.Unmarshal(bytes.TrimSpace(line), &evt); jsonErr != nil {
			// A complete line that won't parse is corrupt, not partial. Ship the
			// good events accumulated so far (to keep cursor order), then skip it so
			// the syncer can't wedge forever on one bad record.
			if !sendBatch() {
				break
			}
			log.Printf("Syncer: skipping unparseable record at offset %d: %v", s.offset, jsonErr)
			s.offset += int64(len(line))
			advanced = true
			continue
		}

		batch = append(batch, evt)
		batchBytes += int64(len(line))
		if len(batch) >= maxEventBatch || batchBytes >= maxEventBatchBytes {
			if !sendBatch() {
				break
			}
		}
	}

	// Ship any remainder that didn't hit a size cap.
	sendBatch()

	if advanced {
		s.writeCursor(s.offset)
	}
}

func (s *Syncer) readCursor() int64 {
	data, err := os.ReadFile(s.cursorPath)
	if err != nil {
		return 0
	}
	var off int64
	if _, err := fmt.Sscanf(string(data), "%d", &off); err != nil {
		return 0
	}
	return off
}

// writeCursor persists the cursor atomically (write-temp-then-rename) so a crash
// can never leave a torn cursor file.
func (s *Syncer) writeCursor(off int64) {
	tmp := s.cursorPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d", off)), 0644); err != nil {
		log.Printf("Syncer: failed to write cursor: %v", err)
		return
	}
	if err := os.Rename(tmp, s.cursorPath); err != nil {
		log.Printf("Syncer: failed to commit cursor: %v", err)
	}
}

// push POSTs a batch of events; the ingestion endpoint accepts a []JobEvent array.
func (s *Syncer) push(batch []events.JobEvent) error {
	url := fmt.Sprintf("%s/api/v1/runs/%s/events", s.APIURL, s.RunID)

	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	return nil
}
