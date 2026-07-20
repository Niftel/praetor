package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/praetordev/events"
)

func TestIngestDiagnosticsIsAllowlistedAndResumable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "diagnostic-events.jsonl")
	line := `{"schema_version":1,"event_type":"HOST_FAILED","timestamp":1,"task_name":"deploy","task_uuid":"task-1","task_action":"copy","host":"web-1","outcome":"failed","failure_code":"task_failed"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(dir, "")
	request := events.ExecutionRequest{ExecutionRunID: uuid.New(), UnifiedJobID: 7}
	runner.ingestDiagnostics(&request, path)
	runner.ingestDiagnostics(&request, path) // cursor prevents replay
	_ = runner.Wal.Close()

	file, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
		if strings.Contains(scanner.Text(), "module_args") || strings.Contains(scanner.Text(), "stdout") {
			t.Fatalf("diagnostic event contains non-allowlisted result data: %s", scanner.Text())
		}
		var event events.JobEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event.Diagnostic == nil || event.Diagnostic.Outcome != "failed" || event.EventType != events.EventHostFailed {
			t.Fatalf("unexpected projected diagnostic: %#v", event)
		}
	}
	if count != 1 {
		t.Fatalf("expected one event after replay, got %d", count)
	}
}
