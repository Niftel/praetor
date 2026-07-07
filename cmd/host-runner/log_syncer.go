package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// maxLogChunk caps the size of a single uploaded chunk so a burst of output is
// shipped as several bounded POSTs rather than one huge request.
const maxLogChunk = 256 * 1024

// LogSyncer streams the host-local stdout.log to the control plane's object
// store, in chunks, as the playbook runs. It mirrors the event Syncer's
// crash-safety: a persistent cursor (byte offset + next chunk seq) advances only
// after a chunk is confirmed stored, so a failed upload or a restart resumes
// from the last acknowledged position instead of dropping or re-sending output.
type LogSyncer struct {
	APIURL string
	RunID  string
	Token  string
	Client *http.Client

	logPath    string
	cursorPath string
	offset     int64 // bytes of stdout.log confirmed stored
	chunkSeq   int64 // next chunk sequence number
}

func NewLogSyncer(jobDir, apiURL, runID, token string) *LogSyncer {
	return &LogSyncer{
		APIURL:     apiURL,
		RunID:      runID,
		Token:      token,
		Client:     &http.Client{Timeout: 10 * time.Second},
		logPath:    filepath.Join(jobDir, "stdout.log"),
		cursorPath: filepath.Join(jobDir, "stdout.cursor"),
	}
}

func (s *LogSyncer) Start(done chan bool) {
	log.Printf("Starting LogSyncer for Run %s to %s", s.RunID, s.APIURL)
	s.offset, s.chunkSeq = s.readCursor()
	if s.offset > 0 {
		log.Printf("LogSyncer: resuming from offset %d (chunk seq %d)", s.offset, s.chunkSeq)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			log.Println("LogSyncer stopping, final flush...")
			s.flush()
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

// flush ships all newly-written stdout, capped at maxLogChunk per chunk. The
// cursor is advanced only past chunks the control plane confirmed storing.
func (s *LogSyncer) flush() {
	f, err := os.Open(s.logPath)
	if err != nil {
		return // stdout.log not created yet
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return
	}

	advanced := false
	for s.offset < info.Size() {
		size := info.Size() - s.offset
		if size > maxLogChunk {
			size = maxLogChunk
		}

		buf := make([]byte, size)
		if _, err := f.ReadAt(buf, s.offset); err != nil && err != io.EOF {
			log.Printf("LogSyncer: read at %d failed: %v", s.offset, err)
			break
		}

		if err := s.push(s.chunkSeq, buf); err != nil {
			log.Printf("LogSyncer: push of chunk %d failed at offset %d (will retry): %v", s.chunkSeq, s.offset, err)
			break // keep cursor parked; retry same chunk next tick
		}

		s.offset += int64(len(buf))
		s.chunkSeq++
		advanced = true
	}

	if advanced {
		s.writeCursor(s.offset, s.chunkSeq)
	}
}

func (s *LogSyncer) readCursor() (offset, seq int64) {
	data, err := os.ReadFile(s.cursorPath)
	if err != nil {
		return 0, 0
	}
	if _, err := fmt.Sscanf(string(data), "%d %d", &offset, &seq); err != nil {
		return 0, 0
	}
	return offset, seq
}

// writeCursor persists "<offset> <seq>" atomically so a crash can never leave a
// torn cursor.
func (s *LogSyncer) writeCursor(offset, seq int64) {
	tmp := s.cursorPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d %d", offset, seq)), 0644); err != nil {
		log.Printf("LogSyncer: failed to write cursor: %v", err)
		return
	}
	if err := os.Rename(tmp, s.cursorPath); err != nil {
		log.Printf("LogSyncer: failed to commit cursor: %v", err)
	}
}

func (s *LogSyncer) push(seq int64, data []byte) error {
	url := fmt.Sprintf("%s/api/v1/runs/%s/logs?seq=%d", s.APIURL, s.RunID, seq)

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ingestion returned status %d", resp.StatusCode)
	}
	return nil
}
