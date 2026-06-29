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
	Client *http.Client

	walPath    string
	cursorPath string
	offset     int64
}

func NewSyncer(jobDir, apiURL, runID string) *Syncer {
	return &Syncer{
		JobDir:     jobDir,
		APIURL:     apiURL,
		RunID:      runID,
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

// flush pushes every complete, not-yet-synced line from the WAL. The cursor is
// advanced (in memory, persisted once at the end) only past lines that were
// confirmed received, so a push failure leaves the cursor parked and the line is
// retried on the next tick.
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
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// No newline-terminated record available: either EOF or a partial
			// trailing write. Never advance past an incomplete record.
			break
		}

		var evt events.JobEvent
		if jsonErr := json.Unmarshal(bytes.TrimSpace(line), &evt); jsonErr != nil {
			// A complete line that won't parse is corrupt, not partial; skip it
			// so the syncer can't wedge forever on one bad record.
			log.Printf("Syncer: skipping unparseable record at offset %d: %v", s.offset, jsonErr)
			s.offset += int64(len(line))
			advanced = true
			continue
		}

		if pushErr := s.push(&evt); pushErr != nil {
			log.Printf("Syncer: push failed at offset %d (will retry): %v", s.offset, pushErr)
			break // keep cursor parked; retry this same line next tick
		}
		s.offset += int64(len(line))
		advanced = true
	}

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

func (s *Syncer) push(evt *events.JobEvent) error {
	url := fmt.Sprintf("%s/api/v1/runs/%s/events", s.APIURL, s.RunID)

	// Wrap in array as the Ingestion Service expects []JobEvent.
	body, err := json.Marshal([]*events.JobEvent{evt})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

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
