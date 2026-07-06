package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// JobTemplateWebhook is what an inbound job-template webhook needs to verify and
// launch.
type JobTemplateWebhook struct {
	Name              string `db:"name"`
	UJTID             *int64 `db:"unified_job_template_id"`
	WebhookEnabled    bool   `db:"webhook_enabled"`
	WebhookKey        string `db:"webhook_key"`
	AllowSimultaneous bool   `db:"allow_simultaneous"`
}

// WorkflowTemplateWebhook is what an inbound workflow webhook needs to verify.
type WorkflowTemplateWebhook struct {
	WebhookEnabled bool   `db:"webhook_enabled"`
	WebhookKey     string `db:"webhook_key"`
}

// PackWebhook is what a pack-rebuild webhook needs to verify.
type PackWebhook struct {
	Name       string         `db:"name"`
	WebhookKey sql.NullString `db:"webhook_key"`
}

// NodeCallbackInfo is what a workflow-node callback needs to authorize a release.
type NodeCallbackInfo struct {
	Status     string `db:"status"`
	EventToken string `db:"event_token"`
}

// WebhookStore is the data-access layer for inbound webhook handling.
type WebhookStore struct {
	db *sqlx.DB
}

func NewWebhookStore(db *sqlx.DB) *WebhookStore { return &WebhookStore{db: db} }

// JobTemplateWebhook loads a job template's webhook config by id.
func (s *WebhookStore) JobTemplateWebhook(ctx context.Context, id int64) (JobTemplateWebhook, error) {
	var t JobTemplateWebhook
	err := s.db.GetContext(ctx, &t,
		`SELECT name, unified_job_template_id, webhook_enabled, webhook_key, allow_simultaneous
		 FROM job_templates WHERE id = $1`, id)
	return t, wrap("WebhookStore.JobTemplateWebhook", err)
}

// ActiveJobCount counts non-terminal jobs for a unified template (concurrency guard).
func (s *WebhookStore) ActiveJobCount(ctx context.Context, unifiedTemplateID int64) (int, error) {
	var active int
	err := s.db.GetContext(ctx, &active,
		`SELECT count(*) FROM unified_jobs
		 WHERE unified_job_template_id = $1 AND status NOT IN ('successful','failed','canceled','error')`,
		unifiedTemplateID)
	return active, wrap("WebhookStore.ActiveJobCount", err)
}

// InsertWebhookJob creates a pending unified_job from a webhook and returns its
// id. The raw error is returned so the caller can detect an active-run conflict.
func (s *WebhookStore) InsertWebhookJob(ctx context.Context, name string, unifiedTemplateID int64, jobArgs []byte) (int64, error) {
	var jobID int64
	err := s.db.QueryRowxContext(ctx, `
		INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at, job_args)
		VALUES ($1, $2, 'pending', now(), $3) RETURNING id`,
		name, unifiedTemplateID, jobArgs).Scan(&jobID)
	return jobID, wrap("WebhookStore.InsertWebhookJob", err)
}

// WorkflowTemplateWebhook loads a workflow template's webhook config by id.
func (s *WebhookStore) WorkflowTemplateWebhook(ctx context.Context, id int64) (WorkflowTemplateWebhook, error) {
	var t WorkflowTemplateWebhook
	err := s.db.GetContext(ctx, &t,
		`SELECT webhook_enabled, webhook_key FROM workflow_templates WHERE id = $1`, id)
	return t, wrap("WebhookStore.WorkflowTemplateWebhook", err)
}

// LaunchWorkflowSnapshot snapshots a workflow template's nodes+edges into a new
// running workflow_jobs run and returns its id (one transaction).
func (s *WebhookStore) LaunchWorkflowSnapshot(ctx context.Context, workflowTemplateID int64) (int64, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return 0, wrap("WebhookStore.LaunchWorkflowSnapshot", err)
	}
	defer tx.Rollback()
	var wjID int64
	if err := tx.QueryRowxContext(ctx,
		`INSERT INTO workflow_jobs (workflow_template_id, status) VALUES ($1, 'running') RETURNING id`, workflowTemplateID).Scan(&wjID); err != nil {
		return 0, wrap("WebhookStore.LaunchWorkflowSnapshot", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workflow_job_nodes (workflow_job_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body, status)
		 SELECT $1, node_key, node_type, job_template_id, name, webhook_url, webhook_body, 'pending' FROM workflow_nodes WHERE workflow_template_id=$2`,
		wjID, workflowTemplateID); err != nil {
		return 0, wrap("WebhookStore.LaunchWorkflowSnapshot", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workflow_job_edges (workflow_job_id, parent_key, child_key, edge_type)
		 SELECT $1, parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$2`,
		wjID, workflowTemplateID); err != nil {
		return 0, wrap("WebhookStore.LaunchWorkflowSnapshot", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, wrap("WebhookStore.LaunchWorkflowSnapshot", err)
	}
	return wjID, nil
}

// PackWebhook loads an execution pack's webhook config by id.
func (s *WebhookStore) PackWebhook(ctx context.Context, id int64) (PackWebhook, error) {
	var t PackWebhook
	err := s.db.GetContext(ctx, &t, `SELECT name, webhook_key FROM execution_packs WHERE id=$1`, id)
	return t, wrap("WebhookStore.PackWebhook", err)
}

// QueuePackRebuild marks a pack pending (git-push rebuild).
func (s *WebhookStore) QueuePackRebuild(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE execution_packs SET status='pending' WHERE id=$1`, id)
	return wrap("WebhookStore.QueuePackRebuild", err)
}

// NodeCallbackInfo loads a workflow job node's status + event token by id.
func (s *WebhookStore) NodeCallbackInfo(ctx context.Context, id int64) (NodeCallbackInfo, error) {
	var n NodeCallbackInfo
	err := s.db.GetContext(ctx, &n,
		`SELECT status, COALESCE(event_token,'') AS event_token FROM workflow_job_nodes WHERE id=$1`, id)
	return n, wrap("WebhookStore.NodeCallbackInfo", err)
}

// ReleaseNode transitions an awaiting_event node to the given result.
func (s *WebhookStore) ReleaseNode(ctx context.Context, id int64, result string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflow_job_nodes SET status=$1 WHERE id=$2 AND status='awaiting_event'`, result, id)
	return wrap("WebhookStore.ReleaseNode", err)
}
