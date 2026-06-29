package tests

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/pkg/events"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/controller/reconciler"
)

// MockLauncher that does nothing
type MockLauncher struct{}

func (m *MockLauncher) Launch(ctx context.Context, run models.ExecutionRun, manifest events.JobManifest) error {
	return nil
}
func (m *MockLauncher) GetStatus(ctx context.Context, runID uuid.UUID) (string, error) {
	return "running", nil // Pretend always running if asked
}
func (m *MockLauncher) StreamLogs(ctx context.Context, runID uuid.UUID, stdout, stderr *os.File) error {
	return nil
}

// MockBuilder that does nothing
type MockBuilder struct{}

func (m *MockBuilder) Build(ctx context.Context, run *models.ExecutionRun) (events.JobManifest, error) {
	return events.JobManifest{}, nil
}

func TestStaleRunProtection(t *testing.T) {
	// Connect to local DB (assumes docker compose is running)
	dbURL := "postgres://postgres:postgres@localhost:5432/praetor?sslmode=disable"
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("Skipping integration test: failed to connect to DB: %v", err)
	}
	defer db.Close()

	// 1. Setup Data
	// Create a dummy project/inventory/template/job hierarchy if needed,
	// or just insert a run if constraints allow?
	// The DB schema likely requires FKs.
	// We'll rely on existing seed data or insert minimal tree.
	// For simplicity, let's assume at least one UnifiedJob exists or insert one.

	// Insert minimal Job
	var jobID int64
	err = db.QueryRow(`INSERT INTO unified_jobs (name, status, created_at) VALUES ('Stale Test Job', 'pending', NOW()) RETURNING id`).Scan(&jobID)
	if err != nil {
		t.Fatalf("Failed to create unified job: %v", err)
	}

	// Insert Stale Run (1 hour old)
	runID := uuid.New()
	_, err = db.Exec(`
		INSERT INTO execution_runs (id, unified_job_id, attempt_number, state, created_at) 
		VALUES ($1, $2, 1, 'pending', NOW() - INTERVAL '1 hour')
	`, runID, jobID)
	if err != nil {
		t.Fatalf("Failed to insert stale run: %v", err)
	}

	// 2. Start Reconciler with 5 min timeout
	rec := reconciler.NewReconciler(db, &MockLauncher{}, &MockBuilder{}, 100*time.Millisecond, 5*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Run for 1 second then stop
		time.Sleep(1 * time.Second)
		rec.Stop()
		cancel()
	}()

	go rec.Start()

	<-ctx.Done()

	// 3. Verify State
	var finalState string
	err = db.Get(&finalState, "SELECT state FROM execution_runs WHERE id = $1", runID)
	if err != nil {
		t.Fatalf("Failed to fetch run state: %v", err)
	}

	if finalState != "failed" {
		t.Errorf("Expected run to be 'failed' due to staleness, got '%s'", finalState)
	} else {
		log.Printf("SUCCESS: Stale run %s was correctly marked as failed", runID)
	}

	// Cleanup
	_, _ = db.Exec("DELETE FROM execution_runs WHERE id = $1", runID)
	_, _ = db.Exec("DELETE FROM unified_jobs WHERE id = $1", jobID)
}
