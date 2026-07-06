// Package store holds the API's data-access layer: SQL lives here, behind small
// interfaces the handlers declare and depend on, so handlers keep only RBAC and
// rendering. Column lists are always explicit (never SELECT *) so a new DB column
// can neither break a scan nor silently change an API response.
package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
)

// Explicit column lists, matching the model structs' db tags (and deliberately
// excluding internal-only columns like unified_jobs.concurrency_key and
// execution_runs.runner_host_id/reconcile_*/credential_id).
const (
	unifiedJobCols   = `id, unified_job_template_id, name, status, current_run_id, created_at, started_at, finished_at, cancel_requested, job_args`
	executionRunCols = `id, unified_job_id, attempt_number, created_at, started_at, finished_at, state, last_heartbeat_at, last_event_seq, persisted_event_seq`
	jobEventCols     = `id, unified_job_id, execution_run_id, seq, event_type, host_id, task_name, play_name, event_data, stdout_snippet, created_at`
)

// JobStore is the data-access layer for the jobs domain.
type JobStore struct {
	db *sqlx.DB
}

func NewJobStore(db *sqlx.DB) *JobStore { return &JobStore{db: db} }

// ListRecent returns the most recent unified jobs (superuser/auditor view).
func (s *JobStore) ListRecent(ctx context.Context, limit int) ([]models.UnifiedJob, error) {
	jobs := []models.UnifiedJob{}
	err := s.db.SelectContext(ctx, &jobs,
		`SELECT `+unifiedJobCols+` FROM unified_jobs ORDER BY created_at DESC LIMIT $1`, limit)
	return jobs, err
}

// ListReadable returns recent unified jobs whose governing template is in tmplIDs.
func (s *JobStore) ListReadable(ctx context.Context, tmplIDs []int64, limit int) ([]models.UnifiedJob, error) {
	jobs := []models.UnifiedJob{}
	if len(tmplIDs) == 0 {
		return jobs, nil
	}
	q, args, err := sqlx.In(`
		SELECT `+prefixed("uj", unifiedJobCols)+`
		FROM unified_jobs uj
		JOIN job_templates jt ON uj.unified_job_template_id = jt.unified_job_template_id
		WHERE jt.id IN (?)
		ORDER BY uj.created_at DESC LIMIT ?`, tmplIDs, limit)
	if err != nil {
		return nil, err
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &jobs, q, args...)
	return jobs, err
}

// GetRun returns a single execution run by id.
func (s *JobStore) GetRun(ctx context.Context, runID uuid.UUID) (models.ExecutionRun, error) {
	var run models.ExecutionRun
	err := s.db.GetContext(ctx, &run,
		`SELECT `+executionRunCols+` FROM execution_runs WHERE id = $1`, runID)
	return run, err
}

// ListEvents returns a run's job events in sequence order.
func (s *JobStore) ListEvents(ctx context.Context, runID uuid.UUID) ([]models.JobEvent, error) {
	events := []models.JobEvent{}
	err := s.db.SelectContext(ctx, &events,
		`SELECT `+jobEventCols+` FROM job_events WHERE execution_run_id = $1 ORDER BY seq ASC`, runID)
	return events, err
}

// TemplateIDForRun resolves the job_templates.id governing a run, via
// unified_job -> unified_job_template_id. ok is false when the run has no
// governing template (e.g. an ad-hoc / inventory-sync job) — that is the ONLY
// no-error miss. A real DB error is returned as an error, never masked as
// "no template": masking it would silently degrade the RBAC decision at the
// callsite (a transient outage would read as an unowned run).
func (s *JobStore) TemplateIDForRun(ctx context.Context, runID uuid.UUID) (int64, bool, error) {
	var jtID int64
	err := s.db.GetContext(ctx, &jtID, `
		SELECT jt.id
		FROM execution_runs er
		JOIN unified_jobs uj ON er.unified_job_id = uj.id
		JOIN job_templates jt ON uj.unified_job_template_id = jt.unified_job_template_id
		WHERE er.id = $1`, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return jtID, true, nil
}

// prefixed qualifies a comma-separated column list with a table alias, e.g.
// prefixed("uj", "id, name") -> "uj.id, uj.name".
func prefixed(alias, cols string) string {
	out := ""
	start := 0
	for i := 0; i <= len(cols); i++ {
		if i == len(cols) || cols[i] == ',' {
			col := cols[start:i]
			// trim surrounding spaces
			for len(col) > 0 && col[0] == ' ' {
				col = col[1:]
			}
			if out != "" {
				out += ", "
			}
			out += alias + "." + col
			start = i + 1
		}
	}
	return out
}
