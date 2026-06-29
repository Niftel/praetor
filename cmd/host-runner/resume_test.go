package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/praetordev/praetor/pkg/events"
)

// fakeControlPlane accepts the host-runner's /events, /logs and /heartbeat
// posts and records the event seqs it received.
type fakeControlPlane struct {
	mu     sync.Mutex
	events []int64
	server *httptest.Server
}

func newFakeControlPlane() *fakeControlPlane {
	f := &fakeControlPlane{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			var batch []events.JobEvent
			_ = json.NewDecoder(r.Body).Decode(&batch)
			f.mu.Lock()
			for _, e := range batch {
				f.events = append(f.events, e.Seq)
			}
			f.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	return f
}

func (f *fakeControlPlane) eventCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.events) }

// writeInterruptedJob lays down a job directory as a crashed runner would have
// left it: a manifest and resume metadata, but no terminal status.json.
func writeInterruptedJob(t *testing.T, root, apiURL string) string {
	t.Helper()
	runID := uuid.New()
	dir := filepath.Join(root, runID.String())
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	req := events.ExecutionRequest{
		ExecutionRunID: runID,
		UnifiedJobID:   1,
		JobManifest: events.JobManifest{
			Inventory:       "localhost ansible_connection=local",
			PlaybookContent: "- hosts: localhost\n  gather_facts: no\n  tasks:\n    - debug: { msg: resumed }\n",
		},
	}
	manifest, _ := json.Marshal(req)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(runnerMeta{RunID: runID.String(), APIURL: apiURL})
	if err := os.WriteFile(filepath.Join(dir, "runner-meta.json"), meta, 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func terminalState(t *testing.T, jobDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(jobDir, "status.json"))
	if err != nil {
		return ""
	}
	var s struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal(data, &s)
	return s.State
}

// TestResumeRunsInterruptedJobAndSkipsFinished is the host-side resume guard:
// a boot scan re-runs an interrupted job to a terminal state and syncs its
// events, and a second scan leaves the now-finished job alone.
func TestResumeRunsInterruptedJobAndSkipsFinished(t *testing.T) {
	root := t.TempDir()
	cp := newFakeControlPlane()
	defer cp.server.Close()

	jobDir := writeInterruptedJob(t, root, cp.server.URL)

	// Boot scan resumes it.
	resumeAll(root)

	if st := terminalState(t, jobDir); st != "successful" && st != "failed" {
		t.Fatalf("after resume, job should have a terminal status.json, got %q", st)
	}
	if cp.eventCount() == 0 {
		t.Fatal("resume should have synced events to the control plane")
	}

	// isComplete now reports done.
	if !isComplete(jobDir) {
		t.Fatal("finished job should be marked complete")
	}

	// A second scan must not re-run it.
	countBefore := cp.eventCount()
	resumeAll(root)
	if got := cp.eventCount(); got != countBefore {
		t.Fatalf("a finished job was re-run on the second scan (events %d -> %d)", countBefore, got)
	}
}
