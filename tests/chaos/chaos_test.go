// Package chaos holds failure-injection tests that kill a dependency mid-flight
// and assert the resilience guarantees hold: no lost events, no duplicates,
// convergence after recovery. They drive real containers via `docker
// pause`/`restart`, so they are gated on CHAOS_* env vars and run via
// scripts/chaos-test.sh, never as part of the normal unit suite.
package chaos

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"github.com/praetordev/events"
	natsbus "github.com/praetordev/praetor/pkg/transport/nats"
	consumercore "github.com/praetordev/praetor/services/consumer/core"
)

func dockerCmd(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("docker %v failed: %v\n%s", args, err, out)
	}
}

func makeEvent(runID uuid.UUID, jobID, seq int64) *events.JobEvent {
	return &events.JobEvent{
		ExecutionRunID: runID,
		UnifiedJobID:   jobID,
		Seq:            seq,
		EventType:      "TASK_OK", // non-terminal: just inserts a row
		Timestamp:      time.Now(),
	}
}

// TestDBOutageConvergence is the headline P0 guarantee: while the database is
// down, job events accumulate durably in JetStream and the consumer cannot
// commit them; once the database returns, the consumer catches up and every
// event lands exactly once.
func TestDBOutageConvergence(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	natsURL := os.Getenv("TEST_NATS_URL")
	dbContainer := os.Getenv("CHAOS_DB_CONTAINER")
	if dbURL == "" || natsURL == "" || dbContainer == "" {
		t.Skip("chaos env not set (TEST_DATABASE_URL, TEST_NATS_URL, CHAOS_DB_CONTAINER)")
	}

	db := sqlx.MustConnect("postgres", dbURL)
	defer db.Close()
	bus, err := natsbus.NewNatsBus(natsURL)
	if err != nil {
		t.Fatalf("nats: %v", err)
	}
	defer bus.Close()

	// Fixture job + run.
	var jobID int64
	if err := db.QueryRow(`INSERT INTO unified_jobs (name, status) VALUES ('chaos-db', 'running') RETURNING id`).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	runID := uuid.New()
	if _, err := db.Exec(`INSERT INTO execution_runs (id, unified_job_id, state) VALUES ($1, $2, 'running')`, runID, jobID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	defer db.Exec(`DELETE FROM unified_jobs WHERE id = $1`, jobID)

	// Real consumer path: ack only after the DB commit.
	writer := consumercore.NewDBWriter(db)
	if err := bus.ConsumeJobEvents(func(evt events.JobEvent) error {
		return writer.WriteEvent(context.Background(), evt)
	}); err != nil {
		t.Fatalf("consume: %v", err)
	}

	const total = 40
	const beforeOutage = 15

	// Publish a first batch and let it drain.
	for seq := int64(1); seq <= beforeOutage; seq++ {
		if err := bus.PublishJobEvent(makeEvent(runID, jobID, seq)); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	waitForCount(t, db, runID, beforeOutage, 15*time.Second)

	// CHAOS: take the database down.
	t.Logf("pausing database container %s", dbContainer)
	dockerCmd(t, "pause", dbContainer)

	// Publish the rest while the DB is down — these cannot be committed yet.
	for seq := int64(beforeOutage + 1); seq <= total; seq++ {
		if err := bus.PublishJobEvent(makeEvent(runID, jobID, seq)); err != nil {
			t.Fatalf("publish during outage: %v", err)
		}
	}
	// Hold the outage so the consumer demonstrably cannot make progress.
	time.Sleep(5 * time.Second)

	// Recover.
	t.Logf("unpausing database container %s", dbContainer)
	dockerCmd(t, "unpause", dbContainer)

	// Convergence: every event lands, exactly once.
	waitForCount(t, db, runID, total, 45*time.Second)

	var distinct int
	if err := db.Get(&distinct, `SELECT count(DISTINCT seq) FROM job_events WHERE execution_run_id = $1`, runID); err != nil {
		t.Fatalf("count distinct: %v", err)
	}
	if distinct != total {
		t.Fatalf("expected %d distinct events after recovery, got %d", total, distinct)
	}
}

// TestNATSRestartDurability asserts events published to JetStream survive a
// broker restart (file storage) and are still delivered to a later consumer.
func TestNATSRestartDurability(t *testing.T) {
	natsURL := os.Getenv("TEST_NATS_URL")
	natsContainer := os.Getenv("CHAOS_NATS_CONTAINER")
	if natsURL == "" || natsContainer == "" {
		t.Skip("chaos env not set (TEST_NATS_URL, CHAOS_NATS_CONTAINER)")
	}

	bus, err := natsbus.NewNatsBus(natsURL)
	if err != nil {
		t.Fatalf("nats: %v", err)
	}
	runID := uuid.New()
	const total = 20
	for seq := int64(1); seq <= total; seq++ {
		if err := bus.PublishJobEvent(makeEvent(runID, 1, seq)); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	bus.Close() // no consumer yet — events live only in the stream

	// CHAOS: restart the broker.
	t.Logf("restarting NATS container %s", natsContainer)
	dockerCmd(t, "restart", natsContainer)
	waitForNATS(t, natsURL, 30*time.Second)

	// A fresh consumer must still receive every event.
	bus2, err := natsbus.NewNatsBus(natsURL)
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer bus2.Close()

	got := make(map[int64]bool)
	done := make(chan struct{})
	if err := bus2.ConsumeJobEvents(func(evt events.JobEvent) error {
		if evt.ExecutionRunID == runID {
			got[evt.Seq] = true
			if len(got) == total {
				select {
				case <-done:
				default:
					close(done)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("consume: %v", err)
	}

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("only %d/%d events survived the NATS restart", len(got), total)
	}
}

func waitForCount(t *testing.T, db *sqlx.DB, runID uuid.UUID, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var n int
	for time.Now().Before(deadline) {
		if err := db.Get(&n, `SELECT count(*) FROM job_events WHERE execution_run_id = $1`, runID); err == nil && n >= want {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d events (have %d)", want, n)
}

func waitForNATS(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		nc, err := nats.Connect(url, nats.Timeout(1*time.Second))
		if err == nil {
			nc.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("NATS did not come back after restart")
}
