package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/events"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/objectstore"
)

type EventPublisher interface {
	PublishJobEvent(event *events.JobEvent) error
	PublishLogChunk(chunk *events.LogChunk) error
}

type IngestionService struct {
	DB        *sqlx.DB
	Publisher EventPublisher
	Store     objectstore.LogStore
}

func NewIngestionService(db *sqlx.DB, pub EventPublisher, store objectstore.LogStore) *IngestionService {
	return &IngestionService{
		DB:        db,
		Publisher: pub,
		Store:     store,
	}
}

// RecordHeartbeat stamps a run's liveness. The reconciler reads
// last_heartbeat_at to distinguish a live long-running job from a lost one. A
// truly terminal run is left untouched (a late heartbeat can't revive it), but
// a 'lost' run whose host has rebooted and resumed will start heartbeating
// again — that revives it to 'running' so the control plane reflects reality
// during the resumed run (its eventual terminal event then finalizes it).
func (s *IngestionService) RecordHeartbeat(ctx context.Context, runID uuid.UUID) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE execution_runs
		SET last_heartbeat_at = now(),
		    state = CASE WHEN state = 'lost' THEN 'running' ELSE state END,
		    finished_at = CASE WHEN state = 'lost' THEN NULL ELSE finished_at END
		WHERE id = $1 AND state NOT IN ('successful', 'failed', 'canceled')`, runID)
	if err != nil {
		return fmt.Errorf("record heartbeat: %w", err)
	}
	return nil
}

// LatestLogSeq returns the highest stored chunk seq for a run, or -1 if none.
// It lets a reader advance its tail cursor without parsing the streamed bytes.
func (s *IngestionService) LatestLogSeq(ctx context.Context, runID uuid.UUID) (int64, error) {
	var seq int64
	err := s.DB.GetContext(ctx, &seq,
		`SELECT COALESCE(MAX(seq), -1) FROM job_output_chunks WHERE execution_run_id = $1`, runID)
	return seq, err
}

// StreamLogs writes the run's stored output, in chunk order, to w. sinceSeq
// supports incremental tailing: a caller polling for live updates passes the
// highest seq it has already seen, and only later chunks are written.
func (s *IngestionService) StreamLogs(ctx context.Context, runID uuid.UUID, sinceSeq int64, w io.Writer) error {
	if s.Store == nil {
		return fmt.Errorf("log store not configured")
	}

	rows, err := s.DB.QueryxContext(ctx, `
		SELECT storage_key FROM job_output_chunks
		WHERE execution_run_id = $1 AND seq > $2
		ORDER BY seq`, runID, sinceSeq)
	if err != nil {
		return fmt.Errorf("list log chunks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		data, err := s.Store.Get(key)
		if err != nil {
			return fmt.Errorf("fetch chunk %s: %w", key, err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return rows.Err()
}

// IngestLogChunk persists a raw stdout chunk to the object store and publishes a
// LogChunk index notification. The bytes are written to durable storage first so
// that, if the index publish fails and the host-runner retries the chunk, the
// re-upload is an idempotent overwrite of the same key and the consumer dedups
// the index row on (execution_run_id, seq).
func (s *IngestionService) IngestLogChunk(ctx context.Context, runID uuid.UUID, seq int64, data []byte) error {
	if s.Store == nil {
		return fmt.Errorf("log store not configured")
	}

	key := objectstore.ChunkKey(runID.String(), seq)
	if err := s.Store.Put(key, data); err != nil {
		return fmt.Errorf("store log chunk: %w", err)
	}

	if err := s.Publisher.PublishLogChunk(&events.LogChunk{
		ExecutionRunID: runID,
		Seq:            seq,
		StorageKey:     key,
		ByteLength:     len(data),
		Timestamp:      time.Now(),
	}); err != nil {
		return fmt.Errorf("publish log chunk: %w", err)
	}
	return nil
}

// IngestEvents persists a batch of events.
func (s *IngestionService) IngestEvents(ctx context.Context, runID uuid.UUID, eventsList []models.JobEvent) error {
	if len(eventsList) == 0 {
		return nil
	}

	// 1. Fetch the UnifiedJobID for this run.
	// We trust the runID from the URL, but the DB requires unified_job_id for the FK in job_events.
	var unifiedJobID int64
	err := s.DB.GetContext(ctx, &unifiedJobID, "SELECT unified_job_id FROM execution_runs WHERE id = $1", runID)
	if err != nil {
		return fmt.Errorf("failed to find execution run %s: %w", runID, err)
	}

	// Now publish to NATS
	for _, event := range eventsList {
		// Ensure system fields match the URL and DB reality
		event.ExecutionRunID = runID
		event.UnifiedJobID = unifiedJobID

		// ID is BIGSERIAL, let DB handle it if 0
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}

		// Ensure EventData is not nil/empty for JSONB column
		if len(event.EventData) == 0 {
			event.EventData = json.RawMessage("{}")
		}

		natsEvent := &events.JobEvent{
			UnifiedJobID:   event.UnifiedJobID,
			ExecutionRunID: event.ExecutionRunID, // Valid uuid.UUID
			EventType:      event.EventType,
			Seq:            event.Seq,
			Timestamp:      event.CreatedAt,
			TaskName:       event.TaskName,
			PlayName:       event.PlayName,
			StdoutSnippet:  event.StdoutSnippet,
			EventData:      event.EventData, // Valid json.RawMessage
		}

		if err := s.Publisher.PublishJobEvent(natsEvent); err != nil {
			log.Printf("Failed to publish event to NATS: %v", err)
			return fmt.Errorf("failed to publish event to NATS: %w", err)
		}
	}

	return nil
}
