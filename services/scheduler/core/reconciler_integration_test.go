package core

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// TestReconcilerHeartbeatAware proves the reconciler distinguishes a live
// long-running job from a lost one:
//   - a job running for an hour but heartbeating recently is left alone;
//   - a run whose heartbeat went stale is marked lost (job -> error);
//   - a run that started but never heartbeated is marked lost.
//
// Requires TEST_DATABASE_URL (migrated); skips otherwise.
func TestReconcilerHeartbeatAware(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping reconciler integration test")
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("cannot reach TEST_DATABASE_URL: %v", err)
	}
	defer db.Close()

	sched := NewScheduler(db, time.Second, nil)

	// newRun inserts a 'running' job + run with the given ages. hbAgo < 0 means
	// "never heartbeated" (NULL last_heartbeat_at).
	newRun := func(startedAgo, hbAgo time.Duration) (int64, uuid.UUID) {
		var jobID int64
		if err := db.QueryRow(`INSERT INTO unified_jobs (name, status) VALUES ('recon-test', 'running') RETURNING id`).Scan(&jobID); err != nil {
			t.Fatalf("insert job: %v", err)
		}
		runID := uuid.New()
		if _, err := db.Exec(`INSERT INTO execution_runs (id, unified_job_id, state) VALUES ($1, $2, 'running')`, runID, jobID); err != nil {
			t.Fatalf("insert run: %v", err)
		}
		if _, err := db.Exec(`UPDATE execution_runs SET started_at = now() - make_interval(secs => $1) WHERE id = $2`, startedAgo.Seconds(), runID); err != nil {
			t.Fatalf("set started_at: %v", err)
		}
		if hbAgo >= 0 {
			if _, err := db.Exec(`UPDATE execution_runs SET last_heartbeat_at = now() - make_interval(secs => $1) WHERE id = $2`, hbAgo.Seconds(), runID); err != nil {
				t.Fatalf("set heartbeat: %v", err)
			}
		}
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM unified_jobs WHERE id = $1`, jobID) })
		return jobID, runID
	}

	aliveJob, aliveRun := newRun(time.Hour, 10*time.Second)    // long-running but healthy
	staleJob, staleRun := newRun(time.Hour, 10*time.Minute)    // heartbeat went stale
	neverJob, neverRun := newRun(10*time.Minute, -1)           // started, never heartbeated

	if err := sched.processTimedOutJobs(); err != nil {
		t.Fatalf("processTimedOutJobs: %v", err)
	}

	// Alive run must be untouched — this is the regression the old blanket
	// timeout caused (it would have failed a healthy hour-long job).
	if got := runState(t, db, aliveRun); got != "running" {
		t.Fatalf("alive long-running run was wrongly reconciled to %q", got)
	}
	if got := jobStatus(t, db, aliveJob); got != "running" {
		t.Fatalf("alive job status changed to %q", got)
	}

	// Stale-heartbeat run is lost; its job is error.
	if got := runState(t, db, staleRun); got != "lost" {
		t.Fatalf("stale run state = %q, want lost", got)
	}
	if got := jobStatus(t, db, staleJob); got != "error" {
		t.Fatalf("stale job status = %q, want error", got)
	}

	// Never-heartbeated run (past the start grace) is lost too.
	if got := runState(t, db, neverRun); got != "lost" {
		t.Fatalf("never-heartbeated run state = %q, want lost", got)
	}
	if got := jobStatus(t, db, neverJob); got != "error" {
		t.Fatalf("never-heartbeated job status = %q, want error", got)
	}
}

func runState(t *testing.T, db *sqlx.DB, runID uuid.UUID) string {
	t.Helper()
	var s string
	if err := db.Get(&s, `SELECT state FROM execution_runs WHERE id = $1`, runID); err != nil {
		t.Fatalf("get run state: %v", err)
	}
	return s
}

func jobStatus(t *testing.T, db *sqlx.DB, jobID int64) string {
	t.Helper()
	var s string
	if err := db.Get(&s, `SELECT status FROM unified_jobs WHERE id = $1`, jobID); err != nil {
		t.Fatalf("get job status: %v", err)
	}
	return s
}
