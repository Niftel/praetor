package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/launch"
)

// WorkflowNode is a node in a workflow template graph (also the create/update input).
type WorkflowNode struct {
	NodeKey       string `json:"node_key" db:"node_key"`
	NodeType      string `json:"node_type" db:"node_type"` // job | approval | webhook_in | webhook_out
	JobTemplateID *int64 `json:"job_template_id" db:"job_template_id"`
	Name          string `json:"name" db:"name"`
	WebhookURL    string `json:"webhook_url" db:"webhook_url"`
	WebhookBody   string `json:"webhook_body" db:"webhook_body"`
}

// WorkflowEdge is an edge in a workflow template/job graph.
type WorkflowEdge struct {
	ParentKey string `json:"parent_key" db:"parent_key"`
	ChildKey  string `json:"child_key" db:"child_key"`
	EdgeType  string `json:"edge_type" db:"edge_type"`
}

// WorkflowSummary is the list view of a workflow template.
type WorkflowSummary struct {
	ID             int64  `json:"id" db:"id"`
	OrganizationID int64  `json:"organization_id" db:"organization_id"`
	Name           string `json:"name" db:"name"`
}

// WorkflowMeta is the template's webhook/concurrency config (for GetWorkflow).
type WorkflowMeta struct {
	Enabled  bool   `db:"webhook_enabled"`
	Service  string `db:"webhook_service"`
	AllowSim bool   `db:"allow_simultaneous"`
}

// WorkflowSpec is the create/update payload for a workflow template + its graph.
type WorkflowSpec struct {
	OrganizationID    int64
	Name              string
	WebhookEnabled    bool
	WebhookService    string
	WebhookKey        string
	AllowSimultaneous bool
	Nodes             []WorkflowNode
	Edges             []WorkflowEdge
}

// WorkflowRun is the list view of a workflow job run.
type WorkflowRun struct {
	ID                 int64      `json:"id" db:"id"`
	WorkflowTemplateID int64      `json:"workflow_template_id" db:"workflow_template_id"`
	TemplateName       string     `json:"template_name" db:"template_name"`
	OrganizationID     int64      `json:"organization_id" db:"organization_id"`
	Status             string     `json:"status" db:"status"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	FinishedAt         *time.Time `json:"finished_at" db:"finished_at"`
}

// WorkflowJobMeta is a run's header detail.
type WorkflowJobMeta struct {
	Org        int64      `db:"organization_id"`
	TemplateID int64      `db:"workflow_template_id"`
	Name       string     `db:"name"`
	Status     string     `db:"status"`
	CreatedAt  time.Time  `db:"created_at"`
	FinishedAt *time.Time `db:"finished_at"`
}

// WorkflowJobNode is a node's live state within a run. CallbackURL is populated by
// the handler (not scanned) while a webhook_in node is awaiting an event.
type WorkflowJobNode struct {
	ID           int64   `json:"id" db:"id"`
	NodeKey      string  `json:"node_key" db:"node_key"`
	NodeType     string  `json:"node_type" db:"node_type"`
	Name         string  `json:"name" db:"name"`
	UnifiedJobID *int64  `json:"unified_job_id" db:"unified_job_id"`
	RunID        *string `json:"run_id" db:"run_id"`
	Status       string  `json:"status" db:"status"`
	EventToken   string  `json:"-" db:"event_token"`
	CallbackURL  string  `json:"callback_url,omitempty" db:"-"`
}

// WorkflowStore is the data-access layer for the workflows domain.
type WorkflowStore struct {
	db *sqlx.DB
}

func NewWorkflowStore(db *sqlx.DB) *WorkflowStore { return &WorkflowStore{db: db} }

func wfNullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// OrgOf returns the org that owns a workflow template.
func (s *WorkflowStore) OrgOf(ctx context.Context, id int64) (int64, bool) {
	var org int64
	err := s.db.GetContext(ctx, &org, `SELECT organization_id FROM workflow_templates WHERE id=$1`, id)
	return org, err == nil
}

// ListByIDs returns the given workflow templates (RBAC-scoped by the caller).
func (s *WorkflowStore) ListByIDs(ctx context.Context, ids []int64) ([]WorkflowSummary, error) {
	rows := []WorkflowSummary{}
	if len(ids) == 0 {
		return rows, nil
	}
	q, args, err := sqlx.In(`SELECT id, organization_id, name FROM workflow_templates WHERE id IN (?) ORDER BY name`, ids)
	if err != nil {
		return nil, wrap("WorkflowStore.ListByIDs", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &rows, q, args...)
	return rows, wrap("WorkflowStore.ListByIDs", err)
}

// insertGraph inserts a spec's nodes and edges for a template id (applying the
// default node/edge types), within a transaction.
func insertGraph(ctx context.Context, tx *sqlx.Tx, templateID int64, spec WorkflowSpec) error {
	for _, n := range spec.Nodes {
		if n.NodeType == "" {
			n.NodeType = "job"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workflow_nodes (workflow_template_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			templateID, n.NodeKey, n.NodeType, n.JobTemplateID, n.Name, wfNullIfEmpty(n.WebhookURL), wfNullIfEmpty(n.WebhookBody)); err != nil {
			return wrap("insertGraph", err)
		}
	}
	for _, e := range spec.Edges {
		if e.EdgeType == "" {
			e.EdgeType = "success"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workflow_node_edges (workflow_template_id, parent_key, child_key, edge_type)
			 VALUES ($1, $2, $3, $4)`, templateID, e.ParentKey, e.ChildKey, e.EdgeType); err != nil {
			return wrap("insertGraph", err)
		}
	}
	return nil
}

// Create inserts a workflow template and its graph in one transaction, returning
// the new template id.
func (s *WorkflowStore) Create(ctx context.Context, spec WorkflowSpec) (int64, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return 0, wrap("WorkflowStore.Create", err)
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowxContext(ctx,
		`INSERT INTO workflow_templates (organization_id, name, webhook_enabled, webhook_service, webhook_key, allow_simultaneous)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		spec.OrganizationID, spec.Name, spec.WebhookEnabled, wfNullIfEmpty(spec.WebhookService), wfNullIfEmpty(spec.WebhookKey), spec.AllowSimultaneous).Scan(&id); err != nil {
		return 0, wrap("WorkflowStore.Create", err)
	}
	if err := insertGraph(ctx, tx, id, spec); err != nil {
		return 0, wrap("WorkflowStore.Create", err)
	}
	return id, tx.Commit()
}

// Update edits a workflow template and replaces its graph wholesale (webhook_key
// preserved unless a new non-empty value is supplied).
func (s *WorkflowStore) Update(ctx context.Context, id int64, spec WorkflowSpec) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return wrap("WorkflowStore.Update", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE workflow_templates SET name=$2, webhook_enabled=$3, webhook_service=$4,
		        webhook_key=COALESCE(NULLIF($5,''), webhook_key), allow_simultaneous=$6, modified_at=now()
		 WHERE id=$1`,
		id, spec.Name, spec.WebhookEnabled, wfNullIfEmpty(spec.WebhookService), wfNullIfEmpty(spec.WebhookKey), spec.AllowSimultaneous); err != nil {
		return wrap("WorkflowStore.Update", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_node_edges WHERE workflow_template_id=$1`, id); err != nil {
		return wrap("WorkflowStore.Update", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_nodes WHERE workflow_template_id=$1`, id); err != nil {
		return wrap("WorkflowStore.Update", err)
	}
	if err := insertGraph(ctx, tx, id, spec); err != nil {
		return wrap("WorkflowStore.Update", err)
	}
	return tx.Commit()
}

// TemplateNodes returns a template's nodes.
func (s *WorkflowStore) TemplateNodes(ctx context.Context, templateID int64) ([]WorkflowNode, error) {
	nodes := []WorkflowNode{}
	err := s.db.SelectContext(ctx, &nodes,
		`SELECT node_key, node_type, job_template_id, name,
		        COALESCE(webhook_url,'') AS webhook_url, COALESCE(webhook_body,'') AS webhook_body
		 FROM workflow_nodes WHERE workflow_template_id=$1`, templateID)
	return nodes, wrap("WorkflowStore.TemplateNodes", err)
}

// TemplateEdges returns a template's edges.
func (s *WorkflowStore) TemplateEdges(ctx context.Context, templateID int64) ([]WorkflowEdge, error) {
	edges := []WorkflowEdge{}
	err := s.db.SelectContext(ctx, &edges,
		`SELECT parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$1`, templateID)
	return edges, wrap("WorkflowStore.TemplateEdges", err)
}

// TemplateMeta returns a template's webhook/concurrency config.
func (s *WorkflowStore) TemplateMeta(ctx context.Context, templateID int64) (WorkflowMeta, error) {
	var wh WorkflowMeta
	err := s.db.GetContext(ctx, &wh,
		`SELECT webhook_enabled, COALESCE(webhook_service,'') AS webhook_service, allow_simultaneous FROM workflow_templates WHERE id=$1`, templateID)
	return wh, wrap("WorkflowStore.TemplateMeta", err)
}

// Delete removes a workflow template.
func (s *WorkflowStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workflow_templates WHERE id=$1`, id)
	return wrap("WorkflowStore.Delete", err)
}

// AllowSimultaneous reports whether a workflow permits overlapping runs.
func (s *WorkflowStore) AllowSimultaneous(ctx context.Context, id int64) bool {
	var allow bool
	_ = s.db.GetContext(ctx, &allow, `SELECT allow_simultaneous FROM workflow_templates WHERE id=$1`, id)
	return allow
}

// ActiveRunCount counts non-terminal runs of a workflow (concurrency guard).
func (s *WorkflowStore) ActiveRunCount(ctx context.Context, id int64) (int, error) {
	var active int
	err := s.db.GetContext(ctx, &active,
		`SELECT count(*) FROM workflow_jobs WHERE workflow_template_id = $1 AND status NOT IN ('successful','failed','canceled','error')`, id)
	return active, wrap("WorkflowStore.ActiveRunCount", err)
}

// LaunchSnapshot snapshots a template's nodes+edges into a running workflow_jobs
// run and returns its id (one transaction).
func (s *WorkflowStore) LaunchSnapshot(ctx context.Context, templateID int64) (int64, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return 0, wrap("WorkflowStore.LaunchSnapshot", err)
	}
	defer tx.Rollback()
	wjID, err := launch.Workflow(ctx, tx, templateID)
	if err != nil {
		return 0, wrap("WorkflowStore.LaunchSnapshot", err)
	}
	return wjID, wrap("WorkflowStore.LaunchSnapshot", tx.Commit())
}

// ListJobsByTemplates returns recent workflow runs for the given templates
// (RBAC-scoped by the caller to the workflows they can read).
func (s *WorkflowStore) ListJobsByTemplates(ctx context.Context, templateIDs []int64) ([]WorkflowRun, error) {
	rows := []WorkflowRun{}
	if len(templateIDs) == 0 {
		return rows, nil
	}
	q, args, err := sqlx.In(`
		SELECT wj.id, wj.workflow_template_id, wt.name AS template_name,
		       wt.organization_id, wj.status, wj.created_at, wj.finished_at
		FROM workflow_jobs wj
		JOIN workflow_templates wt ON wt.id = wj.workflow_template_id
		WHERE wj.workflow_template_id IN (?)
		ORDER BY wj.id DESC LIMIT 100`, templateIDs)
	if err != nil {
		return nil, wrap("WorkflowStore.ListJobsByTemplates", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &rows, q, args...)
	return rows, wrap("WorkflowStore.ListJobsByTemplates", err)
}

// JobMeta returns a run's header (with owning org) by id.
func (s *WorkflowStore) JobMeta(ctx context.Context, id int64) (WorkflowJobMeta, error) {
	var meta WorkflowJobMeta
	err := s.db.GetContext(ctx, &meta, `
		SELECT wt.organization_id, wj.workflow_template_id, wt.name, wj.status, wj.created_at, wj.finished_at
		FROM workflow_jobs wj
		JOIN workflow_templates wt ON wt.id = wj.workflow_template_id
		WHERE wj.id=$1`, id)
	return meta, wrap("WorkflowStore.JobMeta", err)
}

// JobNodes returns a run's nodes with each node's latest execution run id.
func (s *WorkflowStore) JobNodes(ctx context.Context, jobID int64) ([]WorkflowJobNode, error) {
	nodes := []WorkflowJobNode{}
	err := s.db.SelectContext(ctx, &nodes, `
		SELECT wjn.id, wjn.node_key, wjn.node_type,
		       COALESCE(wjn.name, '') AS name, wjn.unified_job_id, wjn.status,
		       COALESCE(wjn.event_token, '') AS event_token,
		       er.id AS run_id
		FROM workflow_job_nodes wjn
		LEFT JOIN LATERAL (
		       SELECT id FROM execution_runs
		       WHERE unified_job_id = wjn.unified_job_id
		       ORDER BY created_at DESC LIMIT 1
		) er ON true
		WHERE wjn.workflow_job_id = $1
		ORDER BY wjn.id`, jobID)
	return nodes, wrap("WorkflowStore.JobNodes", err)
}

// JobEdges returns a run's edges.
func (s *WorkflowStore) JobEdges(ctx context.Context, jobID int64) ([]WorkflowEdge, error) {
	edges := []WorkflowEdge{}
	err := s.db.SelectContext(ctx, &edges,
		`SELECT parent_key, child_key, edge_type FROM workflow_job_edges WHERE workflow_job_id=$1`, jobID)
	return edges, wrap("WorkflowStore.JobEdges", err)
}

// NodeApprovalTemplate returns the workflow template a job node belongs to, so
// approval can be gated on that workflow's approval_role.
func (s *WorkflowStore) NodeApprovalTemplate(ctx context.Context, nodeID int64) (int64, error) {
	var tplID int64
	err := s.db.GetContext(ctx, &tplID, `
		SELECT wt.id
		FROM workflow_job_nodes wjn
		JOIN workflow_jobs wj ON wj.id = wjn.workflow_job_id
		JOIN workflow_templates wt ON wt.id = wj.workflow_template_id
		WHERE wjn.id=$1`, nodeID)
	return tplID, wrap("WorkflowStore.NodeApprovalTemplate", err)
}

// SetNodeApproval transitions an awaiting_approval node to approved/rejected.
func (s *WorkflowStore) SetNodeApproval(ctx context.Context, nodeID int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflow_job_nodes SET status=$1 WHERE id=$2 AND status='awaiting_approval'`, status, nodeID)
	return wrap("WorkflowStore.SetNodeApproval", err)
}
