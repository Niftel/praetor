// Package store holds the API's data-access layer: SQL lives here, behind small
// interfaces the handlers declare and depend on, so handlers keep only RBAC and
// rendering. Column lists are always explicit (never SELECT *) so a new DB column
// can neither break a scan nor silently change an API response.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/launch"
	"github.com/praetordev/models"
)

// Explicit column lists, matching the model structs' db tags (and deliberately
// excluding internal-only columns like unified_jobs.concurrency_key and
// execution_runs.runner_host_id/reconcile_*/credential_id).
const (
	unifiedJobCols   = `id, unified_job_template_id, name, status, current_run_id, created_at, started_at, finished_at, cancel_requested, job_args`
	executionRunCols = `id, unified_job_id, created_at, started_at, finished_at, state, last_heartbeat_at, last_event_seq, persisted_event_seq`
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
	return jobs, wrap("JobStore.ListRecent", err)
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
		return nil, wrap("JobStore.ListReadable", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &jobs, q, args...)
	return jobs, wrap("JobStore.ListReadable", err)
}

// GetRun returns a single execution run by id.
func (s *JobStore) GetRun(ctx context.Context, runID uuid.UUID) (models.ExecutionRun, error) {
	var run models.ExecutionRun
	err := s.db.GetContext(ctx, &run,
		`SELECT `+executionRunCols+` FROM execution_runs WHERE id = $1`, runID)
	return run, wrap("JobStore.GetRun", err)
}

// ListEvents returns a run's job events in sequence order.
func (s *JobStore) ListEvents(ctx context.Context, runID uuid.UUID) ([]models.JobEvent, error) {
	events := []models.JobEvent{}
	err := s.db.SelectContext(ctx, &events,
		`SELECT `+jobEventCols+` FROM job_events WHERE execution_run_id = $1 ORDER BY seq ASC`, runID)
	return events, wrap("JobStore.ListEvents", err)
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
		return 0, false, wrap("JobStore.TemplateIDForRun", err)
	}
	return jtID, true, nil
}

// --- write paths (jobs domain) ---

// LaunchTemplateInfo is what LaunchJob needs to validate a launch: the
// job_templates.id that owns the RBAC roles plus the prompt-on-launch config.
type LaunchTemplateInfo struct {
	ID                   int64           `db:"id"`
	AskVariablesOnLaunch bool            `db:"ask_variables_on_launch"`
	AskLimitOnLaunch     bool            `db:"ask_limit_on_launch"`
	SurveyEnabled        bool            `db:"survey_enabled"`
	SurveySpec           json.RawMessage `db:"survey_spec"`
	AllowSimultaneous    bool            `db:"allow_simultaneous"`
}

// LaunchTemplateInfo loads the launch-time template config by unified template id.
func (s *JobStore) LaunchTemplateInfo(ctx context.Context, unifiedTemplateID int64) (LaunchTemplateInfo, error) {
	var jt LaunchTemplateInfo
	err := s.db.GetContext(ctx, &jt,
		`SELECT id, ask_variables_on_launch, ask_limit_on_launch, survey_enabled, survey_spec, allow_simultaneous
		 FROM job_templates WHERE unified_job_template_id = $1`, unifiedTemplateID)
	if err != nil {
		return jt, fmt.Errorf("load launch template info: %w", err)
	}
	return jt, nil
}

// ActiveJobCount counts non-terminal jobs for a template — the concurrency guard
// for templates that don't allow simultaneous runs.
func (s *JobStore) ActiveJobCount(ctx context.Context, unifiedTemplateID int64) (int, error) {
	var n int
	err := s.db.GetContext(ctx, &n,
		`SELECT count(*) FROM unified_jobs
		 WHERE unified_job_template_id = $1 AND status NOT IN ('successful','failed','canceled','error')`,
		unifiedTemplateID)
	if err != nil {
		return 0, fmt.Errorf("count active jobs: %w", err)
	}
	return n, nil
}

// InsertPendingJob creates a unified_job in 'pending' state (no current_run_id);
// the scheduler picks it up. Returns the new job id. Delegates to pkg/launch,
// the single job-creation site.
func (s *JobStore) InsertPendingJob(ctx context.Context, name string, unifiedTemplateID int64, opts launch.Options) (int64, error) {
	id, err := launch.Job(ctx, s.db, name, &unifiedTemplateID, opts)
	if err != nil {
		return 0, fmt.Errorf("insert pending job: %w", err)
	}
	return id, nil
}

// UnifiedJobIDForRun returns the unified_job_id owning an execution run.
func (s *JobStore) UnifiedJobIDForRun(ctx context.Context, runID uuid.UUID) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT unified_job_id FROM execution_runs WHERE id = $1`, runID).Scan(&id)
	if err != nil {
		return 0, err // caller distinguishes sql.ErrNoRows (404)
	}
	return id, nil
}

// InsertJobEvent persists a host-runner event; evt.UnifiedJobID/ExecutionRunID
// must be set by the caller. Returns the new event id.
func (s *JobStore) InsertJobEvent(ctx context.Context, evt *models.JobEvent) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO job_events (
			unified_job_id, execution_run_id, seq, event_type,
			stdout_snippet, event_data, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		evt.UnifiedJobID, evt.ExecutionRunID, evt.Seq, evt.EventType,
		evt.StdoutSnippet, evt.EventData, evt.CreatedAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert job event: %w", err)
	}
	return id, nil
}

// JobCancelInfo is what CancelJob needs: the current status and the governing
// template (nil for a template-less job).
type JobCancelInfo struct {
	Status               string `db:"status"`
	UnifiedJobTemplateID *int64 `db:"unified_job_template_id"`
}

// JobCancelInfo loads a job's status + governing unified template id.
func (s *JobStore) JobCancelInfo(ctx context.Context, jobID int64) (JobCancelInfo, error) {
	var info JobCancelInfo
	err := s.db.GetContext(ctx, &info,
		`SELECT status, unified_job_template_id FROM unified_jobs WHERE id = $1`, jobID)
	if err != nil {
		return info, fmt.Errorf("load job cancel info: %w", err)
	}
	return info, nil
}

// JobTemplateIDByUnified resolves the job_templates.id owning the RBAC roles from
// a unified_job_template_id. ok is false when no such template row exists.
func (s *JobStore) JobTemplateIDByUnified(ctx context.Context, unifiedTemplateID int64) (int64, bool, error) {
	var jtID int64
	err := s.db.GetContext(ctx, &jtID,
		`SELECT id FROM job_templates WHERE unified_job_template_id = $1`, unifiedTemplateID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("resolve template id: %w", err)
	}
	return jtID, true, nil
}

// FlagCancelRequested sets cancel_requested on a running job; its host-runner
// stops the play cooperatively on its next heartbeat.
func (s *JobStore) FlagCancelRequested(ctx context.Context, jobID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE unified_jobs SET cancel_requested = true WHERE id = $1`, jobID); err != nil {
		return fmt.Errorf("flag cancel requested: %w", err)
	}
	return nil
}

// CancelNotYetRunning cancels a job that hasn't started executing (pending/queued
// /waiting): flag it canceled and terminate any run row already created, in one
// transaction so a job can't be dispatched between the two updates.
func (s *JobStore) CancelNotYetRunning(ctx context.Context, jobID int64) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cancel tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE unified_jobs SET cancel_requested = true, status = 'canceled', finished_at = now() WHERE id = $1`, jobID); err != nil {
		return fmt.Errorf("cancel unified job: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE execution_runs SET state = 'canceled', finished_at = now() WHERE unified_job_id = $1 AND state NOT IN ('successful','failed','canceled')`, jobID); err != nil {
		return fmt.Errorf("cancel execution runs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cancel tx: %w", err)
	}
	return nil
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
