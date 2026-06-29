package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/events"
)

type DBWriter struct {
	DB *sqlx.DB
}

func NewDBWriter(db *sqlx.DB) *DBWriter {
	return &DBWriter{DB: db}
}

// WriteLogChunk indexes a log-chunk reference into job_output_chunks. The chunk
// bytes already live durably in the object store; this row is the pointer. The
// ON CONFLICT makes it idempotent so a redelivered or re-uploaded chunk is a
// no-op, which is what lets the consumer ack-after-commit.
func (w *DBWriter) WriteLogChunk(ctx context.Context, chunk events.LogChunk) error {
	_, err := w.DB.ExecContext(ctx, `
		INSERT INTO job_output_chunks (execution_run_id, seq, storage_key, byte_length, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (execution_run_id, seq) DO NOTHING`,
		chunk.ExecutionRunID, chunk.Seq, chunk.StorageKey, chunk.ByteLength, chunk.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert job_output_chunk failed: %w", err)
	}
	return nil
}

// WriteEvent projects a JobEvent into the database.
func (w *DBWriter) WriteEvent(ctx context.Context, evt events.JobEvent) error {
	tx, err := w.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Insert into job_event table
	// Note: We used int64 for ID in models, but typically events might be inserted with DEFAULT id.
	// We need to map JobEvent fields to DB columns.
	eventDataJSON, _ := json.Marshal(evt.EventData)

	// ON CONFLICT makes the write idempotent: the (execution_run_id, seq) unique
	// constraint means a redelivered or replayed event is silently skipped
	// rather than failing the transaction. This is what allows the consumer to
	// safely ack-after-commit and tolerate at-least-once delivery.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO job_events (
			unified_job_id, execution_run_id, seq, event_type,
			host_id, task_name, play_name, event_data, stdout_snippet, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (execution_run_id, seq) DO NOTHING`,
		evt.UnifiedJobID, evt.ExecutionRunID, evt.Seq, evt.EventType,
		nil, evt.TaskName, evt.PlayName, eventDataJSON, evt.StdoutSnippet, evt.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert job_event failed: %w", err)
	}

	// 2. Update execution_run state
	if err := w.updateRunState(ctx, tx, evt); err != nil {
		return fmt.Errorf("update run state failed: %w", err)
	}

	return tx.Commit()
}

func (w *DBWriter) updateRunState(ctx context.Context, tx *sqlx.Tx, evt events.JobEvent) error {
	var newState string
	var newStatus string // for unified_job
	finished := false

	switch evt.EventType {
	case "JOB_STARTED":
		newState = "running"
		newStatus = "running"
	case "JOB_COMPLETED":
		// Check successful/failed based on event data or convention?
		// For MVP assuming success if completed, but ideally we check rc.
		newState = "successful"
		newStatus = "successful"
		finished = true
	case "JOB_FAILED":
		newState = "failed"
		newStatus = "failed"
		finished = true
	default:
		// Normal task events don't change state
		return nil
	}

	// Compute the finish timestamp only for terminal events; COALESCE keeps the
	// earliest started_at / first finished_at across duplicate or replayed
	// events.
	var finishedAt interface{}
	if finished {
		finishedAt = evt.Timestamp
	}

	// The `state NOT IN (<terminal>)` guard makes the projection monotonic: once
	// a run reaches a true terminal state we never overwrite it. Combined with
	// COALESCE/GREATEST this means an out-of-order or replayed event (e.g. a
	// redelivered JOB_STARTED arriving after JOB_COMPLETED) cannot regress final
	// state. Crucially, 'lost' (run) and 'error' (job) are NOT terminal: they are
	// the reconciler's provisional verdict for "the host stopped heartbeating".
	// If that host reboots, resumes the play, and reports a real terminal event,
	// it must win — so those provisional states are excluded from the guard and
	// recoverable. finished_at uses COALESCE($3, finished_at) so a recovering
	// terminal event replaces the reconciler's lost-detection timestamp with the
	// actual completion time (events are deduped by seq, so no double-write).
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_runs SET
			state = $1,
			started_at = COALESCE(started_at, $2),
			finished_at = COALESCE($3, finished_at),
			last_event_seq = GREATEST(last_event_seq, $4)
		WHERE id = $5
		  AND state NOT IN ('successful', 'failed', 'canceled')`,
		newState, evt.Timestamp, finishedAt, evt.Seq, evt.ExecutionRunID,
	); err != nil {
		log.Printf("Failed to update execution_run %s: %v", evt.ExecutionRunID, err)
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE unified_jobs SET
			status = $1,
			started_at = COALESCE(started_at, $2),
			finished_at = COALESCE($3, finished_at)
		WHERE id = $4
		  AND status NOT IN ('successful', 'failed', 'canceled')`,
		newStatus, evt.Timestamp, finishedAt, evt.UnifiedJobID,
	); err != nil {
		log.Printf("Failed to update unified_job %d: %v", evt.UnifiedJobID, err)
		return err
	}

	return nil
}
