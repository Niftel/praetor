package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/praetordev/praetor/pkg/events"
)

// Runner orchestrates the local job execution
type Runner struct {
	JobDir string
	Wal    *WAL
}

func NewRunner(jobDir string) *Runner {
	return &Runner{
		JobDir: jobDir,
		Wal:    NewWAL(filepath.Join(jobDir, "events.jsonl")),
	}
}

func (r *Runner) Execute() error {
	// 1. Read Manifest
	manifestPath := filepath.Join(r.JobDir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	var req events.ExecutionRequest
	if err := json.Unmarshal(manifestBytes, &req); err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	defer r.Wal.Close()

	log.Printf("Executing run %s (Job %d)", req.ExecutionRunID, req.UnifiedJobID)

	// Emit JOB_STARTED event immediately so the job transitions to 'running'
	// This ensures that if we fail later (e.g., git not found), the timeout mechanism
	// can mark the job as failed instead of leaving it stuck in 'queued'.
	startEvent := events.JobEvent{
		ExecutionRunID: req.ExecutionRunID,
		UnifiedJobID:   req.UnifiedJobID,
		Seq:            1,
		EventType:      "JOB_STARTED",
		Timestamp:      time.Now(),
	}
	if err := r.Wal.Append(&startEvent); err != nil {
		log.Printf("Warning: failed to write JOB_STARTED event: %v", err)
	}

	// 2. Prepare Environment (e.g. write playbook file if inline)
	// 2. Prepare Environment
	playbookPath := filepath.Join(r.JobDir, "playbook.yml")

	if req.JobManifest.PlaybookContent != "" {
		if err := os.WriteFile(playbookPath, []byte(req.JobManifest.PlaybookContent), 0644); err != nil {
			return fmt.Errorf("failed to write inline playbook: %w", err)
		}
	} else if req.JobManifest.ProjectURL != "" {
		// New: Support Git Cloning
		// For MVP: shallow clone to temporary dir or subfolder?
		// We are in r.JobDir. Let's clone into "project" subdir.
		projectDir := filepath.Join(r.JobDir, "project")

		log.Printf("Cloning project from %s into %s", req.JobManifest.ProjectURL, projectDir)

		gitCmd := exec.Command("git", "clone", "--depth=1", req.JobManifest.ProjectURL, projectDir)
		if out, err := gitCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git clone failed: %v, output: %s", err, string(out))
		}

		// Adjust playbook path relative to connection?
		// If req.JobManifest.Playbook is "site.yml", it's now "project/site.yml"
		if req.JobManifest.Playbook != "" {
			playbookPath = filepath.Join(projectDir, req.JobManifest.Playbook)
		} else {
			playbookPath = filepath.Join(projectDir, "site.yml") // Default
		}

	} else if req.JobManifest.Playbook != "" {
		// Legacy/Bundle path assumption
		playbookPath = req.JobManifest.Playbook
	}

	// 3. Prepare Inventory (write to inventory.ini)
	inventoryPath := filepath.Join(r.JobDir, "inventory.ini")
	if req.JobManifest.Inventory != "" {
		if err := os.WriteFile(inventoryPath, []byte(req.JobManifest.Inventory), 0644); err != nil {
			return fmt.Errorf("failed to write inventory: %w", err)
		}
	} else {
		// Default local
		_ = os.WriteFile(inventoryPath, []byte("localhost ansible_connection=local"), 0644)
	}

	// 4. Run Ansible. Raw stdout/stderr is written to stdout.log on disk; the
	// log syncer ships it to the object store in chunks. We deliberately no
	// longer emit per-line stdout events into the WAL — bulk output belongs in
	// the object store, not the control-plane database. Only structured
	// lifecycle events (started/completed/failed) flow through the event WAL.
	// Enable the checkpoint callback (if the plugin is deployed) and, when a
	// checkpoint from an interrupted run is present, resume the play: skip
	// completed tasks and restore registered vars.
	playArgs := []string{"-i", inventoryPath}
	if resume := resumeArgs(r.JobDir); resume != nil {
		log.Printf("Resuming play at task %q (restoring checkpointed vars)", resume[1])
		playArgs = append(playArgs, resume...)
	}
	playArgs = append(playArgs, playbookPath)

	cmd := exec.Command("ansible-playbook", playArgs...)
	cmd.Env = append(os.Environ(), "ANSIBLE_FORCE_COLOR=1")
	cmd.Env = append(cmd.Env, checkpointEnv(r.JobDir)...)

	// Append (not truncate): on a resume after interruption this preserves the
	// earlier output and keeps the log syncer's byte cursor valid; the resumed
	// run's output is appended and shipped as the next chunks.
	stdoutFile, err := os.OpenFile(filepath.Join(r.JobDir, "stdout.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open stdout.log: %w", err)
	}
	defer stdoutFile.Close()
	cmd.Stdout = io.MultiWriter(stdoutFile, os.Stdout)
	cmd.Stderr = cmd.Stdout

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	finalState := "successful"
	eventType := "JOB_COMPLETED"
	if err != nil {
		finalState = "failed"
		eventType = "JOB_FAILED"
		log.Printf("Ansible execution failed: %v", err)
	}

	msgEnd := fmt.Sprintf("Job finished in %v. State: %s", duration, finalState)
	r.Wal.Append(&events.JobEvent{
		UnifiedJobID:   req.UnifiedJobID,
		ExecutionRunID: req.ExecutionRunID,
		EventType:      eventType,
		Timestamp:      time.Now(),
		Seq:            2,
		StdoutSnippet:  &msgEnd,
	})

	// Write status.json
	status := map[string]interface{}{
		"execution_run_id": req.ExecutionRunID,
		"state":            finalState,
		"completed_at":     time.Now(),
		"max_seq":          2,
	}
	statusBytes, _ := json.Marshal(status)
	_ = os.WriteFile(filepath.Join(r.JobDir, "status.json"), statusBytes, 0644)

	// Heartbeating during execution is driven from main.go (it targets the run,
	// not just the host) so the reconciler can tell a live long-running job from
	// a lost one. The runner returns once the playbook finishes; the deferred
	// syncer/heartbeat shutdown in main.go then performs a final flush.
	return err
}

// WAL is an append-only, fsync'd write-ahead log of job events. On the host it
// is the primary source of truth during a control-plane outage, so every append
// is flushed to stable storage before returning and each record is written in a
// single syscall (line + newline together) so a concurrent reader — the syncer —
// never observes a half-written record.
type WAL struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

func NewWAL(path string) *WAL {
	w := &WAL{path: path}
	w.f, _ = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	return w
}

func (w *WAL) Append(evt *events.JobEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.f == nil {
		if w.f, err = os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err != nil {
			return err
		}
	}

	if _, err := w.f.Write(data); err != nil {
		return err
	}
	// fsync: a host crash must not lose an event the runner believes is durable.
	return w.f.Sync()
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
