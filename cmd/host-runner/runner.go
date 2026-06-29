package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

	// 4. Run Ansible
	// We use 'ansible-playbook' directly.
	cmd := exec.Command("ansible-playbook", "-i", inventoryPath, playbookPath)
	cmd.Env = append(os.Environ(), "ANSIBLE_FORCE_COLOR=1")

	// ... [omitted stdout capture setup] ...

	stdoutFile, _ := os.Create(filepath.Join(r.JobDir, "stdout.log"))
	defer stdoutFile.Close()

	// Create a pipe to capture output for streaming events
	pr, pw := io.Pipe()

	// Write to file, stdout, and our pipe
	mw := io.MultiWriter(stdoutFile, os.Stdout, pw)
	cmd.Stdout = mw
	cmd.Stderr = mw

	msgStart := "Host runner started playbook execution"
	r.Wal.Append(&events.JobEvent{
		UnifiedJobID:   req.UnifiedJobID, // Set UnifiedJobID
		ExecutionRunID: req.ExecutionRunID,
		EventType:      "JOB_STARTED",
		Timestamp:      time.Now(),
		Seq:            1,
		StdoutSnippet:  &msgStart,
	})

	start := time.Now()

	// Run command in background so we can stream
	cmdErrChan := make(chan error, 1)
	go func() {
		defer pw.Close()
		cmdErrChan <- cmd.Run()
	}()

	// Stream logs
	scanner := bufio.NewScanner(pr)

	// Partition sequence space to avoid collisions between parallel runners
	hostname, _ := os.Hostname()
	h := fnv.New32a()
	h.Write([]byte(hostname))
	// Use top bits of hash to spread out? Or just modulo.
	// 50 concurrent hosts max assumption?
	// Offset = (hash % 100) * 1,000,000
	bucket := int64(h.Sum32() % 100)
	var currentSeq int64 = (bucket * 1000000) + 2

	for scanner.Scan() {
		text := fmt.Sprintf("[%s] %s", hostname, scanner.Text())
		r.Wal.Append(&events.JobEvent{
			UnifiedJobID:   req.UnifiedJobID, // Set UnifiedJobID
			ExecutionRunID: req.ExecutionRunID,
			EventType:      "JOB_STDOUT",
			Timestamp:      time.Now(),
			Seq:            currentSeq,
			StdoutSnippet:  &text,
		})
		currentSeq++
	}

	// Wait for command to finish (it triggers pipe close)
	err = <-cmdErrChan
	duration := time.Since(start)

	finalState := "successful"
	eventType := "JOB_COMPLETED"
	if err != nil {
		finalState = "failed"
		eventType = "JOB_FAILED"
		log.Printf("Ansible execution failed: %v", err)
	}

	msgEnd := fmt.Sprintf("[%s] Job finished in %v. State: %s", hostname, duration, finalState)
	r.Wal.Append(&events.JobEvent{
		UnifiedJobID:   req.UnifiedJobID, // Set UnifiedJobID
		ExecutionRunID: req.ExecutionRunID,
		EventType:      eventType, // Correctly report failure
		Timestamp:      time.Now(),
		Seq:            currentSeq, // Use next seq
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

	// Start heartbeat loop if we have the necessary info
	if req.JobManifest.RunnerHostID > 0 && req.JobManifest.APIURL != "" {
		log.Printf("Starting heartbeat loop for host %d to %s", req.JobManifest.RunnerHostID, req.JobManifest.APIURL)
		go r.heartbeatLoop(req.JobManifest.RunnerHostID, req.JobManifest.APIURL)
		// Block forever to keep agent running
		select {}
	}

	return err
}

func (r *Runner) heartbeatLoop(hostID int64, apiURL string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Send initial heartbeat immediately
	r.sendHeartbeat(hostID, apiURL)

	for range ticker.C {
		r.sendHeartbeat(hostID, apiURL)
	}
}

func (r *Runner) sendHeartbeat(hostID int64, apiURL string) {
	url := fmt.Sprintf("%s/api/v1/hosts/%d/runner-heartbeat", apiURL, hostID)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Printf("Failed to create heartbeat request: %v", err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Heartbeat failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Heartbeat returned status %d", resp.StatusCode)
	} else {
		log.Printf("Heartbeat sent successfully for host %d", hostID)
	}
}

// WAL represents the Write-Ahead Log
type WAL struct {
	Path string
}

func NewWAL(path string) *WAL {
	return &WAL{Path: path}
}

func (w *WAL) Append(evt *events.JobEvent) error {
	f, err := os.OpenFile(w.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		return err
	}
	_, err = f.WriteString("\n")
	return err
}
