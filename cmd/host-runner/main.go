package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/praetordev/events"
)

var (
	jobDir     = flag.String("job-dir", "", "Path to a single job directory to run")
	apiURL     = flag.String("api-url", "", "URL of the Praetor ingestion endpoint")
	runID      = flag.String("run-id", "", "Execution Run ID")
	resumeRoot = flag.String("resume-root", "", "Resume every unfinished job under this directory (run at host boot)")
)

func main() {
	flag.Parse()

	if *resumeRoot != "" {
		resumeAll(*resumeRoot)
		return
	}

	if *jobDir == "" {
		log.Fatal("one of --job-dir or --resume-root is required")
	}
	if err := runJob(*jobDir, *apiURL, *runID); err != nil {
		log.Printf("Host Runner exited with error: %v", err)
		os.Exit(1)
	}
}

// walFormat is the on-disk format version of a job directory's WAL/state
// (events.jsonl, the byte cursors, status.json, checkpoint.json). Bump it ONLY
// for a STRUCTURAL/breaking change. Additive JSON changes — new fields or new
// event_type values — must NOT bump it: readers ignore unknown fields and the
// consumer's projection switch no-ops on unknown events, so those stay
// compatible on their own. A runner understands every format <= walFormat and
// refuses to resume a job written by a newer format (see resumeAll).
const walFormat = 1

// runnerMeta is the small bit of state, persisted in the job directory, that a
// boot-time resume needs but cannot otherwise recover (the run id is also the
// directory name; the ingestion URL is only known at bootstrap time). WALVersion
// records the format the job dir was written with, so a resuming runner can
// refuse a format newer than it understands.
type runnerMeta struct {
	RunID      string `json:"run_id"`
	APIURL     string `json:"api_url"`
	WALVersion int    `json:"wal_version"`
}

// runJob executes a single job in jobDir. It is identical for a fresh run and a
// resume after interruption: all durable state (manifest, WAL, stdout, sync
// cursors) lives in the directory, so re-invoking simply continues from disk.
func runJob(jobDir, apiURL, runID string) error {
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return err
	}

	if logFile, err := os.OpenFile(filepath.Join(jobDir, "runner.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
		defer func() { log.SetOutput(os.Stderr); logFile.Close() }()
	}

	// Refuse to run two runners for the same job at once (e.g. a resume scan
	// racing a fresh bootstrap). The lock is released when this process exits.
	lock, err := acquireJobLock(jobDir)
	if err != nil {
		log.Printf("job %s already has an active runner; skipping", jobDir)
		return nil
	}
	defer lock.release()

	// Persist resume metadata before doing any work so an interruption at any
	// later point is recoverable.
	writeRunnerMeta(jobDir, runnerMeta{RunID: runID, APIURL: apiURL, WALVersion: walFormat})

	log.Printf("Running job dir %s (run %s)", jobDir, runID)

	// The play runs under a cancelable context; the heartbeat loop cancels it when
	// the control plane reports the job was canceled.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if apiURL != "" && runID != "" {
		// The per-run ingestion token lives in the 0600 manifest (never in argv or
		// the world-readable runner-meta). Both fresh runs and boot-time resumes go
		// through here and the manifest is still on disk, so reading it here covers
		// both. An empty token (older manifest / no shared secret) sends no header.
		token := readIngestToken(jobDir)
		syncer := NewSyncer(jobDir, apiURL, runID, token)
		logSyncer := NewLogSyncer(jobDir, apiURL, runID, token)
		done := make(chan bool, 1)
		logDone := make(chan bool, 1)
		hbDone := make(chan bool, 1)
		finished := make(chan bool, 1)
		logFinished := make(chan bool, 1)
		go func() { syncer.Start(done); finished <- true }()
		go func() { logSyncer.Start(logDone); logFinished <- true }()
		go runHeartbeat(apiURL, runID, token, hbDone, cancel)
		defer func() {
			hbDone <- true
			log.Println("Waiting for syncers to finish...")
			time.Sleep(100 * time.Millisecond)
			// The play is done and its WAL on disk holds every event + the terminal
			// status. Keep the syncers running until that WAL is fully delivered to the
			// control plane (or a bounded deadline) so an ingestion outage during/after
			// the run doesn't strand the result — delivery is part of the job.
			waitForDelivery(jobDir, syncDrainDeadline)
			done <- true
			logDone <- true
			<-finished
			<-logFinished
			if jobDelivered(jobDir) {
				log.Println("Syncers finished; results delivered.")
			} else {
				log.Printf("Syncers stopping with results NOT yet delivered after %s — a resume will re-deliver from the WAL.", syncDrainDeadline)
			}
		}()
	}

	// A job whose status.json is already terminal ran to completion in a prior
	// invocation; do NOT re-run the play (that would re-execute tasks). Just let the
	// syncers above drain its persisted WAL. This is the recovery path for a run that
	// finished while ingestion was unreachable and is now being re-driven.
	if isTerminalStatus(jobDir) {
		log.Printf("job %s already terminal — delivering persisted results without re-running", jobDir)
		return nil
	}

	return NewRunner(jobDir, apiURL).Execute(ctx)
}

// resumeAll scans root for job directories that did not reach a terminal state
// and resumes each from its persisted state. It is intended to run at host boot
// so a machine restart does not abandon in-flight work.
func resumeAll(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("resume: cannot read %s: %v", root, err)
		return
	}

	resumed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if isComplete(dir) {
			continue // already finished, nothing to do
		}
		meta, err := readRunnerMeta(dir)
		if err != nil {
			log.Printf("resume: skipping %s (no runner metadata): %v", dir, err)
			continue
		}
		// Refuse a job dir written by a newer WAL format than this runner
		// understands (a downgrade / mixed-fleet mistake). Misreading it could
		// corrupt state or replay wrongly, so leave it untouched for the
		// reconciler (or a matching-version runner) to harvest safely.
		if meta.WALVersion > walFormat {
			log.Printf("resume: skipping %s — WAL format v%d is newer than this runner supports (v%d); leaving it for the reconciler", dir, meta.WALVersion, walFormat)
			continue
		}
		rid := meta.RunID
		if rid == "" {
			rid = e.Name() // the directory name is the execution_run_id
		}
		log.Printf("resume: resuming interrupted job %s", dir)
		if err := runJob(dir, meta.APIURL, rid); err != nil {
			log.Printf("resume: job %s failed: %v", dir, err)
		}
		resumed++
	}
	log.Printf("resume: scan complete (%d job(s) resumed)", resumed)
}

// isTerminalStatus reports whether the play itself reached a terminal state, per
// status.json. A missing or non-terminal status means the play was interrupted.
func isTerminalStatus(jobDir string) bool {
	data, err := os.ReadFile(filepath.Join(jobDir, "status.json"))
	if err != nil {
		return false
	}
	var s struct {
		State string `json:"state"`
	}
	if json.Unmarshal(data, &s) != nil {
		return false
	}
	switch s.State {
	case "successful", "failed", "canceled":
		return true
	}
	return false
}

// isComplete reports whether a job is fully done: it both reached a terminal state
// AND delivered its results (events + logs) to the control plane. A terminal-but-
// undelivered job — one that finished while ingestion was unreachable — is NOT
// complete, so resumeAll re-drives its syncers rather than stranding the outcome.
// Delivery is part of the job the pushed engine owns, not a best-effort side effect:
// losing ingestion must never lose a job's result, only delay when it is reported.
func isComplete(jobDir string) bool {
	return isTerminalStatus(jobDir) && jobDelivered(jobDir)
}

// delivered reports whether a WAL file has been fully shipped: its persisted sync
// cursor has reached the file's end. A missing WAL counts as delivered (nothing to
// ship). Compares on-disk cursor vs WAL size, so it is race-free w.r.t. the syncer.
func delivered(walPath, cursorPath string) bool {
	fi, err := os.Stat(walPath)
	if err != nil {
		return true
	}
	var off int64
	if data, rerr := os.ReadFile(cursorPath); rerr == nil {
		_, _ = fmt.Sscanf(string(data), "%d", &off)
	}
	return off >= fi.Size()
}

// jobDelivered reports whether both the event WAL (events.jsonl) and the stdout log
// (stdout.log) have been fully synced to the control plane.
func jobDelivered(jobDir string) bool {
	return delivered(filepath.Join(jobDir, "events.jsonl"), filepath.Join(jobDir, "events.cursor")) &&
		delivered(filepath.Join(jobDir, "stdout.log"), filepath.Join(jobDir, "stdout.cursor"))
}

// syncDrainDeadline bounds how long a finished runner keeps retrying delivery of its
// WAL through an ingestion outage before exiting. A boot-time resume (or any later
// re-invocation) re-drives anything still undelivered, so this only needs to be long
// enough to ride out a routine blip/restart without leaving a zombie forever.
const syncDrainDeadline = 30 * time.Minute

// waitForDelivery blocks until the job's WAL is fully synced or the deadline
// elapses, while the still-running syncer goroutines keep flushing. It returns
// immediately when nothing is outstanding, so a healthy run pays no penalty.
func waitForDelivery(jobDir string, deadline time.Duration) {
	if jobDelivered(jobDir) {
		return
	}
	log.Printf("results not yet delivered (ingestion unreachable?); retrying for up to %s", deadline)
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		time.Sleep(time.Second)
		if jobDelivered(jobDir) {
			log.Println("results delivered.")
			return
		}
	}
}

func writeRunnerMeta(jobDir string, m runnerMeta) {
	if data, err := json.Marshal(m); err == nil {
		_ = os.WriteFile(filepath.Join(jobDir, "runner-meta.json"), data, 0644)
	}
}

func readRunnerMeta(jobDir string) (runnerMeta, error) {
	var m runnerMeta
	data, err := os.ReadFile(filepath.Join(jobDir, "runner-meta.json"))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(data, &m)
}

// readIngestToken pulls the per-run ingestion bearer token out of the 0600
// manifest.json in the job dir. Best-effort: a missing/unparseable manifest or
// absent token yields "" (the syncers then send no Authorization header), so a
// job whose manifest predates this field still runs — it simply fails auth if
// ingestion now requires a token, which is the correct fail-closed behaviour.
func readIngestToken(jobDir string) string {
	data, err := os.ReadFile(filepath.Join(jobDir, "manifest.json"))
	if err != nil {
		return ""
	}
	var req events.ExecutionRequest
	if json.Unmarshal(data, &req) != nil {
		return ""
	}
	return req.JobManifest.IngestToken
}

// jobLock is an advisory flock on a per-job lock file; it is released when the
// process exits (so a crashed runner does not block a later resume).
type jobLock struct{ f *os.File }

func acquireJobLock(jobDir string) (*jobLock, error) {
	f, err := os.OpenFile(filepath.Join(jobDir, "runner.lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return &jobLock{f: f}, nil
}

func (l *jobLock) release() {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}
