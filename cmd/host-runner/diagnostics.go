package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/praetordev/events"
)

type ansibleDiagnostic struct {
	SchemaVersion int     `json:"schema_version"`
	EventType     string  `json:"event_type"`
	Timestamp     float64 `json:"timestamp"`
	PlayName      string  `json:"play_name"`
	TaskName      string  `json:"task_name"`
	TaskUUID      string  `json:"task_uuid"`
	TaskAction    string  `json:"task_action"`
	Host          string  `json:"host"`
	Outcome       string  `json:"outcome"`
	Changed       bool    `json:"changed"`
	DurationMS    int64   `json:"duration_ms"`
	FailureCode   string  `json:"failure_code"`
}

// ingestDiagnostics turns the callback's allowlisted JSONL records into the
// durable event WAL. It intentionally has no field for Ansible result data.
func (r *Runner) ingestDiagnostics(req *events.ExecutionRequest, path string) {
	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: open diagnostic events: %v", err)
		}
		return
	}
	defer file.Close()
	cursorPath := filepath.Join(filepath.Dir(path), "diagnostic-events.cursor")
	if cursor, err := os.ReadFile(cursorPath); err == nil {
		if offset, err := strconv.ParseInt(strings.TrimSpace(string(cursor)), 10, 64); err == nil {
			if _, err := file.Seek(offset, 0); err != nil {
				log.Printf("Warning: seek diagnostic events: %v", err)
				return
			}
		}
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record ansibleDiagnostic
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || record.SchemaVersion != events.CurrentDiagnosticSchemaVersion {
			log.Printf("Warning: ignored invalid diagnostic event")
			continue
		}
		event := events.JobEvent{
			ExecutionRunID: req.ExecutionRunID,
			UnifiedJobID:   req.UnifiedJobID,
			Seq:            r.seq.next(),
			EventType:      record.EventType,
			Timestamp:      time.UnixMilli(int64(record.Timestamp * 1000)).UTC(),
			PlayName:       stringPointer(record.PlayName),
			TaskName:       stringPointer(record.TaskName),
			Host:           stringPointer(record.Host),
			Diagnostic: &events.ExecutionDiagnostic{
				SchemaVersion: record.SchemaVersion,
				TaskUUID:      record.TaskUUID,
				TaskAction:    record.TaskAction,
				Outcome:       record.Outcome,
				Changed:       record.Changed,
				DurationMS:    record.DurationMS,
				FailureCode:   record.FailureCode,
			},
		}
		if err := r.Wal.Append(&event); err != nil {
			log.Printf("Warning: failed to write %s diagnostic event: %v", record.EventType, err)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Warning: read diagnostic events: %v", err)
		return
	}
	if stat, err := file.Stat(); err == nil {
		if err := os.WriteFile(cursorPath, []byte(strconv.FormatInt(stat.Size(), 10)), 0o600); err != nil {
			log.Printf("Warning: persist diagnostic event cursor: %v", err)
		}
	}
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
