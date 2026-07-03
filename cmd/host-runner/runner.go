package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/praetordev/praetor/pkg/events"
)

// Runner orchestrates the local job execution
type Runner struct {
	JobDir string
	APIURL string // ingestion endpoint, for shipping fact-cache results
	Wal    *WAL
	seq    *seqCounter
}

func NewRunner(jobDir, apiURL string) *Runner {
	walPath := filepath.Join(jobDir, "events.jsonl")
	return &Runner{
		JobDir: jobDir,
		APIURL: apiURL,
		Wal:    NewWAL(walPath),
		seq:    newSeqCounter(walPath),
	}
}

// emit appends a lifecycle event to the WAL with the next durable sequence
// number. The syncer ships it to the control plane like any other event.
func (r *Runner) emit(req *events.ExecutionRequest, eventType, host, msg string, data json.RawMessage) {
	evt := events.JobEvent{
		ExecutionRunID: req.ExecutionRunID,
		UnifiedJobID:   req.UnifiedJobID,
		Seq:            r.seq.next(),
		EventType:      eventType,
		Timestamp:      time.Now(),
	}
	if host != "" {
		evt.Host = &host
	}
	if msg != "" {
		evt.StdoutSnippet = &msg
	}
	if data != nil {
		evt.EventData = data
	}
	if err := r.Wal.Append(&evt); err != nil {
		log.Printf("Warning: failed to write %s event: %v", eventType, err)
	}
}

func (r *Runner) Execute(ctx context.Context) error {
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

	// Identify the host this runner is on for the lifecycle narration below.
	host := req.JobManifest.RunnerHost
	if host == "" {
		if hn, err := os.Hostname(); err == nil {
			host = hn
		}
	}

	// Detect a resume up front: a usable checkpoint means a previous invocation
	// was interrupted (commonly a host reboot) and we are continuing it. We reuse
	// this below instead of re-reading the checkpoint.
	resume := resumeArgs(r.JobDir)

	// RUNNER_ONLINE — the host-runner is live on the target. Reaching this line is
	// itself proof the agentless SSH bootstrap worked: the binary was pushed over
	// SSH and started with no pre-installed agent. This is the first thing a user
	// watching the run sees, and it has no equivalent in a fleet-based tool.
	onlineMsg := fmt.Sprintf("Host runner online on %s — deployed over SSH, no agent pre-installed", host)
	onlineData, _ := json.Marshal(map[string]interface{}{"host": host, "agentless": true, "resumed": resume != nil})
	r.emit(&req, events.EventRunnerOnline, host, onlineMsg, onlineData)

	// RESUMED_FROM_CHECKPOINT — we picked up an interrupted play. resume[1] is the
	// task we restart at; every earlier task is skipped because it already ran.
	if resume != nil {
		rmsg := fmt.Sprintf("Resumed after interruption — skipping completed tasks, continuing at %q", resume[1])
		rdata, _ := json.Marshal(map[string]interface{}{"host": host, "resume_at": resume[1]})
		r.emit(&req, events.EventResumedFromCheckpoint, host, rmsg, rdata)
	}

	// Emit JOB_STARTED so the job transitions to 'running'. This ensures that if
	// we fail later (e.g., git not found), the timeout mechanism can mark the job
	// as failed instead of leaving it stuck in 'queued'.
	r.emit(&req, events.EventJobStarted, host, "", nil)

	// 2. Prepare Environment (e.g. write playbook file if inline)
	// 2. Prepare Environment
	playbookPath := filepath.Join(r.JobDir, "playbook.yml")
	var galaxyPathEnv []string // ANSIBLE_COLLECTIONS_PATH/ROLES_PATH for the cache

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

		// Install the project's Ansible Galaxy requirements (roles/collections)
		// into the content-addressed cache; the returned env points the play at it.
		pathEnv, gerr := installGalaxyRequirements(projectDir, galaxyEnv(req.JobManifest.GalaxyServers))
		if gerr != nil {
			return fmt.Errorf("galaxy requirements: %w", gerr)
		}
		galaxyPathEnv = pathEnv

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
	if req.JobManifest.Limit != "" {
		playArgs = append(playArgs, "--limit", req.JobManifest.Limit)
	}
	if len(req.JobManifest.ExtraVars) > 0 {
		varsPath := filepath.Join(r.JobDir, "extra_vars.json")
		if b, err := json.Marshal(req.JobManifest.ExtraVars); err == nil {
			if err := os.WriteFile(varsPath, b, 0644); err == nil {
				playArgs = append(playArgs, "-e", "@"+varsPath)
			} else {
				log.Printf("Warning: could not write extra_vars: %v", err)
			}
		}
	}
	if resume != nil {
		log.Printf("Resuming play at task %q (restoring checkpointed vars)", resume[1])
		playArgs = append(playArgs, resume...)
	}
	// Privilege-escalation password (if the Machine credential carries one): the
	// scheduler renders it into CredentialFiles; write it to a 0600 file and let
	// ansible-playbook read it via --become-password-file (never on the cmdline).
	if pw := req.JobManifest.CredentialFiles["ANSIBLE_BECOME_PASSWORD_FILE"]; pw != "" {
		pwPath := filepath.Join(r.JobDir, "become_pass")
		if err := os.WriteFile(pwPath, []byte(pw), 0o600); err == nil {
			playArgs = append(playArgs, "--become-password-file", pwPath)
		} else {
			log.Printf("Warning: could not write become password file: %v", err)
		}
	}
	playArgs = append(playArgs, playbookPath)

	// Use Praetor's self-contained runtime (pushed onto the host by the executor)
	// so the target needs no pre-installed Ansible/Python. Falls back to a system
	// ansible-playbook if no runtime is present.
	ansiblePlaybook, ansibleInterpreter := resolveAnsible(req.JobManifest.ExecutionPack)

	// Run under a context so a cancel request (detected by the heartbeat loop)
	// terminates the play. Put ansible-playbook in its own process group and, on
	// cancel, SIGTERM the whole group so its ssh/module children die too; WaitDelay
	// escalates to SIGKILL if it doesn't exit promptly.
	cmd := exec.CommandContext(ctx, ansiblePlaybook, playArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = append(os.Environ(), "ANSIBLE_FORCE_COLOR=1")
	// Point Ansible at the bundled interpreter explicitly (no system symlinks —
	// the runtime stays entirely under /opt/praetor). This makes module execution
	// on the runner host use the bundled Python.
	if ansibleInterpreter != "" {
		cmd.Env = append(cmd.Env, "ANSIBLE_PYTHON_INTERPRETER="+ansibleInterpreter)
	}
	cmd.Env = append(cmd.Env, checkpointEnv(r.JobDir)...)
	cmd.Env = append(cmd.Env, galaxyPathEnv...) // point the play at the cached collections/roles
	// A fresh run is launched by the bootstrap, whose nohup shell exports the
	// SSH env. A resume after a host reboot is launched by the systemd unit,
	// which exports nothing — so a resumed multi-host play would have no key to
	// reach its targets. The bootstrap always copies the job's key into the job
	// dir, so point Ansible at it ourselves (and disable host-key prompts),
	// making both fresh and resumed runs self-sufficient.
	cmd.Env = append(cmd.Env, "ANSIBLE_HOST_KEY_CHECKING=False")
	if key := filepath.Join(r.JobDir, "id_rsa"); fileExists(key) {
		cmd.Env = append(cmd.Env, "ANSIBLE_PRIVATE_KEY_FILE="+key)
	}
	// Apply the Machine credential's rendered env to the play, so the credential's
	// identity (remote user, privilege escalation) governs the connections to the
	// managed hosts — not just the executor's bootstrap hop. ANSIBLE_REMOTE_USER
	// and ANSIBLE_BECOME_METHOD / ANSIBLE_BECOME_USER come from the credential's
	// injectors; a per-host ansible_user in the inventory still wins.
	for k, v := range req.JobManifest.CredentialEnv {
		if k == "" || v == "" {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Turn on become whenever the credential specifies an escalation method (the
	// injectors carry the method/user but not the on/off switch itself).
	if req.JobManifest.CredentialEnv["ANSIBLE_BECOME_METHOD"] != "" {
		cmd.Env = append(cmd.Env, "ANSIBLE_BECOME=True")
	}
	// Fact caching: preload stored facts into a jsonfile cache the play can read,
	// and point Ansible at it so freshly-gathered facts are written back there.
	if req.JobManifest.UseFactCache {
		writeCachedFacts(r.JobDir, req.JobManifest.CachedFacts)
		cmd.Env = append(cmd.Env, factCacheEnv(r.JobDir)...)
	}

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

	// Narrate task-level durability while the play runs: a goroutine watches the
	// checkpoint file and emits a CHECKPOINT_SAVED event each time the play
	// advances to a new resumable task.
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	go func() { watchCheckpoints(r.JobDir, &req, r.Wal, r.seq, host, stopWatch); close(watchDone) }()

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	close(stopWatch)
	<-watchDone

	// Ship any facts Ansible gathered into the cache back to the control plane.
	if req.JobManifest.UseFactCache {
		postFacts(r.APIURL, req.ExecutionRunID.String(), collectFacts(r.JobDir))
	}

	finalState := "successful"
	eventType := events.EventJobCompleted
	if err != nil {
		// A canceled context means the operator asked to stop — report it as
		// canceled, not a failure, so the run's terminal state reflects intent.
		if ctx.Err() == context.Canceled {
			finalState = "canceled"
			eventType = events.EventJobCanceled
			log.Printf("Ansible execution canceled by request")
		} else {
			finalState = "failed"
			eventType = events.EventJobFailed
			log.Printf("Ansible execution failed: %v", err)
		}
	}

	msgEnd := fmt.Sprintf("Job finished in %v. State: %s", duration, finalState)
	r.emit(&req, eventType, host, msgEnd, nil)

	// Write status.json
	status := map[string]interface{}{
		"execution_run_id": req.ExecutionRunID,
		"state":            finalState,
		"completed_at":     time.Now(),
		"max_seq":          r.seq.current(),
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
