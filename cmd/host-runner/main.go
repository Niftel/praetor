package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
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

// runnerMeta is the small bit of state, persisted in the job directory, that a
// boot-time resume needs but cannot otherwise recover (the run id is also the
// directory name; the ingestion URL is only known at bootstrap time).
type runnerMeta struct {
	RunID  string `json:"run_id"`
	APIURL string `json:"api_url"`
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
	writeRunnerMeta(jobDir, runnerMeta{RunID: runID, APIURL: apiURL})

	log.Printf("Running job dir %s (run %s)", jobDir, runID)

	if apiURL != "" && runID != "" {
		syncer := NewSyncer(jobDir, apiURL, runID)
		logSyncer := NewLogSyncer(jobDir, apiURL, runID)
		done := make(chan bool, 1)
		logDone := make(chan bool, 1)
		hbDone := make(chan bool, 1)
		finished := make(chan bool, 1)
		logFinished := make(chan bool, 1)
		go func() { syncer.Start(done); finished <- true }()
		go func() { logSyncer.Start(logDone); logFinished <- true }()
		go runHeartbeat(apiURL, runID, hbDone)
		defer func() {
			hbDone <- true
			log.Println("Waiting for syncers to finish...")
			time.Sleep(100 * time.Millisecond)
			done <- true
			logDone <- true
			<-finished
			<-logFinished
			log.Println("Syncers finished.")
		}()
	}

	return NewRunner(jobDir).Execute()
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

// isComplete reports whether a job already reached a terminal state, per its
// status.json. A missing or non-terminal status means the job was interrupted.
func isComplete(jobDir string) bool {
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
