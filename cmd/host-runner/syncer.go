package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/praetordev/praetor/pkg/events"
)

type Syncer struct {
	JobDir string
	APIURL string
	RunID  string
	Client *http.Client
}

func NewSyncer(jobDir, apiURL, runID string) *Syncer {
	return &Syncer{
		JobDir: jobDir,
		APIURL: apiURL,
		RunID:  runID,
		Client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *Syncer) Start(done chan bool) {
	log.Printf("Starting Syncer for Run %s to %s", s.RunID, s.APIURL)

	filename := s.JobDir + "/events.jsonl"
	file, err := os.Open(filename)
	if err != nil {
		// File might not exist yet, wait
		time.Sleep(1 * time.Second)
		file, err = os.Open(filename)
		if err != nil {
			log.Printf("Syncer: Failed to open events file: %v", err)
			return
		}
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	// Create ticker for periodic checks
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			log.Println("Syncer stopping...")
			s.flush(reader) // Attempt one last read
			return
		case <-ticker.C:
			s.flush(reader)
		}
	}
}

func (s *Syncer) flush(reader *bufio.Reader) {
	for {
		line, err := reader.ReadBytes('\n')

		if len(line) > 0 {
			// Try to process even if err is EOF (partial line or just no newline at EOF)
			// Parse just to verify (and maybe batch in future)
			var evt events.JobEvent
			if parseErr := json.Unmarshal(line, &evt); parseErr != nil {
				log.Printf("Syncer: Failed to parse line: %v (len=%d)", parseErr, len(line))
				// If strictly invalid JSON, we skip.
				// But we continue to check err at bottom.
			} else {
				// Valid event, push it
				if pushErr := s.push(&evt); pushErr != nil {
					log.Printf("Syncer: Failed to push event %d: %v", evt.Seq, pushErr)
				}
			}
		}

		if err != nil {
			return // EOF or error, wait for next tick
		}
	}
}

func (s *Syncer) push(evt *events.JobEvent) error {
	url := fmt.Sprintf("%s/api/v1/runs/%s/events", s.APIURL, s.RunID)

	// Wrap in array as Ingestion Service expects []JobEvent
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
