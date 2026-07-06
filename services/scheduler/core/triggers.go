package core

import (
	"context"
	"database/sql"
	"fmt"
)

// sqlExec is satisfied by both *sqlx.DB and *sqlx.Tx, so launchTarget works inside
// a schedule's transaction or standalone for an event trigger.
type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// logExec runs a state-update statement and logs — never swallows — a failure.
// The scheduler's per-node and per-job status writes aren't transactional with the
// rest of a tick, so a silent failure lets the DB and reality diverge (a job or
// workflow node stuck in the wrong state); at minimum that must be visible.
func logExec(ctx context.Context, ex sqlExec, query string, args ...interface{}) {
	if _, err := ex.ExecContext(ctx, query, args...); err != nil {
		logger.Error("state update failed", "err", err, "query", query)
	}
}

// launchTarget starts a trigger's target: a workflow run (snapshotting its nodes,
// as LaunchWorkflow does) or an ordinary job from a unified job template. Exactly
// one of wfID / ujtID must be set.
func launchTarget(ctx context.Context, ex sqlExec, name string, wfID, ujtID *int64) error {
	switch {
	case wfID != nil:
		var wjID int64
		if err := ex.QueryRowContext(ctx,
			`INSERT INTO workflow_jobs (workflow_template_id, status) VALUES ($1,'running') RETURNING id`, *wfID).Scan(&wjID); err != nil {
			return err
		}
		// Snapshot the graph into the run so later template edits don't affect it.
		if _, err := ex.ExecContext(ctx,
			`INSERT INTO workflow_job_nodes (workflow_job_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body, status)
			 SELECT $1, node_key, node_type, job_template_id, name, webhook_url, webhook_body, 'pending' FROM workflow_nodes WHERE workflow_template_id=$2`,
			wjID, *wfID); err != nil {
			return err
		}
		_, err := ex.ExecContext(ctx,
			`INSERT INTO workflow_job_edges (workflow_job_id, parent_key, child_key, edge_type)
			 SELECT $1, parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$2`,
			wjID, *wfID)
		return err
	case ujtID != nil:
		_, err := ex.ExecContext(ctx,
			`INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at) VALUES ($1,$2,'pending',now())`,
			name, *ujtID)
		return err
	default:
		return fmt.Errorf("trigger %q has no target", name)
	}
}

// processEventTriggers fires enabled event triggers whose matching job has reached
// the relevant terminal state. A trigger only sees jobs created after it (no
// retroactive firing) and fires at most once per source job (event_trigger_fires).
func (s *Scheduler) processEventTriggers(ctx context.Context) {
	type match struct {
		TriggerID int64  `db:"trigger_id"`
		Name      string `db:"name"`
		WfID      *int64 `db:"workflow_template_id"`
		UjtID     *int64 `db:"unified_job_template_id"`
		JobID     int64  `db:"job_id"`
	}
	var matches []match
	// Candidate (trigger, terminal-job) pairs not yet fired. event_type maps to the
	// job's terminal status; source_ujt_id, when set, restricts which jobs qualify.
	if err := s.DB.SelectContext(ctx, &matches, `
		SELECT et.id AS trigger_id, et.name, et.workflow_template_id, et.unified_job_template_id, uj.id AS job_id
		FROM event_triggers et
		JOIN unified_jobs uj
		  ON uj.created_at > et.created_at
		 AND (et.source_ujt_id IS NULL OR uj.unified_job_template_id = et.source_ujt_id)
		 AND (
		      (et.event_type = 'job_succeeded' AND uj.status = 'successful')
		   OR (et.event_type = 'job_failed'    AND uj.status IN ('failed','error'))
		   OR (et.event_type = 'job_finished'  AND uj.status IN ('successful','failed','error'))
		 )
		WHERE et.enabled
		  AND NOT EXISTS (SELECT 1 FROM event_trigger_fires f WHERE f.trigger_id = et.id AND f.source_job_id = uj.id)
		ORDER BY uj.id
		LIMIT 50`); err != nil {
		return
	}
	for _, m := range matches {
		// Claim the (trigger, job) pair; only the insert that wins launches, so a
		// racing tick can't double-fire.
		res, err := s.DB.ExecContext(ctx,
			`INSERT INTO event_trigger_fires (trigger_id, source_job_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			m.TriggerID, m.JobID)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		name := fmt.Sprintf("%s (event: job %d)", m.Name, m.JobID)
		if err := launchTarget(ctx, s.DB, name, m.WfID, m.UjtID); err != nil {
			logger.Error("event trigger launch failed", "trigger_id", m.TriggerID, "err", err)
			continue
		}
		logger.Info("event trigger fired", "trigger_id", m.TriggerID, "job_id", m.JobID)
	}
}
