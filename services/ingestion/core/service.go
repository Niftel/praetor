package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"github.com/praetordev/praetor/pkg/plog"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/credentials"
	"github.com/praetordev/praetor/pkg/events"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/objectstore"
)

// logger is the ingestion package component logger (handler installed by pkg/plog).
var logger = plog.New("ingestion")

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
// a provisionally-failed run whose host is demonstrably alive again should be
// revived to 'running' so the control plane reflects reality until the eventual
// terminal event finalizes it. Both provisional states qualify: 'lost' (host
// rebooted and resumed) and 'reconciling' (a transient blip moved it there, but
// the host is still heartbeating) — otherwise a reconciling run would sit stale
// until the reconciler next SSHes in, even though it's plainly alive.
// ResolveRunCredentials decrypts and returns the AWX-style injectors for the
// Machine credential the scheduler snapshotted onto this run (execution_runs.
// credential_id). Resolution is strictly run-scoped: a caller can only obtain the
// credential that run was dispatched with, never an arbitrary one, and only while
// the run is still live (not terminal). The plaintext is returned for the
// executor's in-memory use — it is never persisted here or logged.
func (s *IngestionService) ResolveRunCredentials(ctx context.Context, runID uuid.UUID) (env, files map[string]string, err error) {
	var row struct {
		CredentialID *int64 `db:"credential_id"`
		State        string `db:"state"`
	}
	if e := s.DB.GetContext(ctx, &row,
		`SELECT credential_id, state FROM execution_runs WHERE id = $1`, runID); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("run not found")
		}
		return nil, nil, e
	}
	switch row.State {
	case "successful", "failed", "canceled", "lost":
		return nil, nil, fmt.Errorf("run is not live (%s)", row.State)
	}
	if row.CredentialID == nil {
		return nil, nil, fmt.Errorf("run has no credential")
	}
	return credentials.ResolveInjectors(ctx, s.DB, *row.CredentialID)
}

// IsRunnable reports whether a run may still be bootstrapped/executed — it exists
// and has not reached a terminal or reconciler-owned state. The executor calls
// this before bootstrapping so a launch that was reaped (queued-timeout) or
// canceled while sitting in the work queue is not run as a ghost after the fact.
// A missing run is treated as not runnable.
func (s *IngestionService) IsRunnable(ctx context.Context, runID uuid.UUID) (bool, error) {
	var state string
	if err := s.DB.GetContext(ctx, &state,
		`SELECT state FROM execution_runs WHERE id = $1`, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	switch state {
	case "successful", "failed", "canceled", "lost":
		return false, nil
	}
	return true, nil
}

func (s *IngestionService) RecordHeartbeat(ctx context.Context, runID uuid.UUID) (bool, error) {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE execution_runs
		SET last_heartbeat_at = now(),
		    state = CASE WHEN state IN ('lost', 'reconciling') THEN 'running' ELSE state END,
		    finished_at = CASE WHEN state IN ('lost', 'reconciling') THEN NULL ELSE finished_at END
		WHERE id = $1 AND NOT run_is_terminal(state)`, runID)
	if err != nil {
		return false, fmt.Errorf("record heartbeat: %w", err)
	}
	// Report back whether the operator asked to cancel this run's job, so the
	// host-runner can stop the play cooperatively (it has no other channel).
	var cancel bool
	if qerr := s.DB.GetContext(ctx, &cancel, `
		SELECT uj.cancel_requested FROM unified_jobs uj
		JOIN execution_runs er ON er.unified_job_id = uj.id WHERE er.id = $1`, runID); qerr != nil {
		return false, nil // best-effort: a lookup failure must not fail the heartbeat
	}
	return cancel, nil
}

// StoreFacts upserts the facts a run gathered, keyed by host. Each entry's host
// name is resolved to a host_id within the run's inventory; names that don't map
// to a host in that inventory are ignored.
func (s *IngestionService) StoreFacts(ctx context.Context, runID uuid.UUID, facts map[string]json.RawMessage) error {
	if len(facts) == 0 {
		return nil
	}
	var inventoryID *int64
	err := s.DB.GetContext(ctx, &inventoryID, `
		SELECT jt.inventory_id
		FROM execution_runs er
		JOIN unified_jobs uj ON uj.id = er.unified_job_id
		JOIN job_templates jt ON jt.unified_job_template_id = uj.unified_job_template_id
		WHERE er.id = $1`, runID)
	if err != nil || inventoryID == nil {
		return nil // no inventory => nowhere to attach facts
	}

	for host, f := range facts {
		if _, err := s.DB.ExecContext(ctx, `
			INSERT INTO host_facts (host_id, facts, modified_at)
			SELECT h.id, $3::jsonb, now() FROM hosts h
			WHERE h.inventory_id = $1 AND h.name = $2
			ON CONFLICT (host_id) DO UPDATE SET facts = EXCLUDED.facts, modified_at = now()`,
			*inventoryID, host, []byte(f)); err != nil {
			logger.Error("facts upsert for host failed", "host", host, "err", err)
		}
	}
	return nil
}

// UpsertInventory parses `ansible-inventory --list` JSON and upserts its hosts,
// groups, and memberships into the given inventory (idempotent, so re-syncing
// updates in place). Host names that already exist keep their id; new ones are
// inserted. Variables come from _meta.hostvars.
func (s *IngestionService) UpsertInventory(ctx context.Context, inventoryID int64, data []byte) error {
	var inv map[string]json.RawMessage
	if err := json.Unmarshal(data, &inv); err != nil {
		return fmt.Errorf("parse inventory json: %w", err)
	}

	hostvars := map[string]json.RawMessage{}
	if meta, ok := inv["_meta"]; ok {
		var m struct {
			HostVars map[string]json.RawMessage `json:"hostvars"`
		}
		_ = json.Unmarshal(meta, &m)
		hostvars = m.HostVars
	}

	allHosts := map[string]bool{}
	groups := map[string][]string{} // real group -> hosts
	for key, raw := range inv {
		if key == "_meta" {
			continue
		}
		var g struct {
			Hosts []string `json:"hosts"`
		}
		_ = json.Unmarshal(raw, &g)
		for _, h := range g.Hosts {
			allHosts[h] = true
		}
		if key != "all" && key != "ungrouped" && len(g.Hosts) > 0 {
			groups[key] = g.Hosts
		}
	}
	for h := range hostvars {
		allHosts[h] = true
	}

	hostID := map[string]int64{}
	for h := range allHosts {
		vars := hostvars[h]
		if len(vars) == 0 {
			vars = json.RawMessage("{}")
		}
		var id int64
		if err := s.DB.GetContext(ctx, &id, `SELECT id FROM hosts WHERE inventory_id=$1 AND name=$2`, inventoryID, h); err != nil {
			if ierr := s.DB.QueryRowContext(ctx,
				`INSERT INTO hosts (inventory_id, name, variables) VALUES ($1, $2, $3::jsonb) RETURNING id`,
				inventoryID, h, []byte(vars)).Scan(&id); ierr != nil {
				logger.Error("sync insert host failed", "host", h, "err", ierr)
				continue
			}
		} else {
			if _, err := s.DB.ExecContext(ctx, `UPDATE hosts SET variables=$2::jsonb, modified_at=now() WHERE id=$1`, id, []byte(vars)); err != nil {
				logger.Error("sync update host vars failed", "host", h, "err", err)
			}
		}
		hostID[h] = id
	}

	for gname, hosts := range groups {
		var gid int64
		if err := s.DB.GetContext(ctx, &gid, `SELECT id FROM groups WHERE inventory_id=$1 AND name=$2`, inventoryID, gname); err != nil {
			if ierr := s.DB.QueryRowContext(ctx,
				`INSERT INTO groups (inventory_id, name, created_at, modified_at) VALUES ($1, $2, now(), now()) RETURNING id`,
				inventoryID, gname).Scan(&gid); ierr != nil {
				logger.Error("sync insert group failed", "group", gname, "err", ierr)
				continue
			}
		}
		for _, h := range hosts {
			if hid, ok := hostID[h]; ok {
				if _, err := s.DB.ExecContext(ctx,
					`INSERT INTO host_groups (host_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, hid, gid); err != nil {
					logger.Error("sync link host to group failed", "host", h, "group", gname, "err", err)
				}
			}
		}
	}

	if _, err := s.DB.ExecContext(ctx, `UPDATE inventory_sources SET last_synced_at=now() WHERE inventory_id=$1`, inventoryID); err != nil {
		logger.Error("sync mark inventory synced failed", "inventory_id", inventoryID, "err", err)
	}
	logger.Info("inventory synced", "inventory_id", inventoryID, "hosts", len(hostID), "groups", len(groups))
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
	EventsIngested.Add(float64(len(eventsList)))

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
			logger.Error("publish event to NATS failed", "err", err)
			return fmt.Errorf("failed to publish event to NATS: %w", err)
		}
	}

	return nil
}
