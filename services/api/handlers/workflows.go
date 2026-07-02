package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

type WorkflowsResource struct {
	DB *sqlx.DB
	*Authorizer
}

func NewWorkflowsResource(db *sqlx.DB) *WorkflowsResource {
	return &WorkflowsResource{DB: db, Authorizer: NewAuthorizer(db)}
}

type workflowNode struct {
	NodeKey       string `json:"node_key" db:"node_key"`
	NodeType      string `json:"node_type" db:"node_type"` // job | approval | webhook_in | webhook_out
	JobTemplateID *int64 `json:"job_template_id" db:"job_template_id"`
	Name          string `json:"name" db:"name"`
	WebhookURL    string `json:"webhook_url" db:"webhook_url"`   // webhook_out
	WebhookBody   string `json:"webhook_body" db:"webhook_body"` // webhook_out
}

type workflowEdge struct {
	ParentKey string `json:"parent_key" db:"parent_key"`
	ChildKey  string `json:"child_key" db:"child_key"`
	EdgeType  string `json:"edge_type" db:"edge_type"`
}

// nullIfEmpty stores NULL for an empty optional string instead of "".
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// orgOfWorkflow returns the org that owns a workflow template.
func (rs *WorkflowsResource) orgOfWorkflow(r *http.Request, id int64) (int64, bool) {
	var org int64
	err := rs.DB.GetContext(r.Context(), &org, `SELECT organization_id FROM workflow_templates WHERE id=$1`, id)
	return org, err == nil
}

// ListWorkflows GET /api/v1/workflow-templates
func (rs *WorkflowsResource) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	type wf struct {
		ID             int64  `json:"id" db:"id"`
		OrganizationID int64  `json:"organization_id" db:"organization_id"`
		Name           string `json:"name" db:"name"`
	}
	rows := []wf{}
	// Honor the object-role model: a user only sees workflows in organizations
	// they can read. Superusers/auditors get everything via readableIDs.
	orgIDs, err := rs.readableIDs(r, rbac.ContentTypeOrganization)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if len(orgIDs) > 0 {
		q, args, _ := sqlx.In(
			`SELECT id, organization_id, name FROM workflow_templates WHERE organization_id IN (?) ORDER BY name`, orgIDs)
		q = rs.DB.Rebind(q)
		if err := rs.DB.SelectContext(r.Context(), &rows, q, args...); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
	}
	render.JSON(w, r, rows)
}

// CreateWorkflow POST /api/v1/workflow-templates
func (rs *WorkflowsResource) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrganizationID int64          `json:"organization_id"`
		Name           string         `json:"name"`
		WebhookEnabled bool           `json:"webhook_enabled"`
		WebhookService string         `json:"webhook_service"`
		WebhookKey     string         `json:"webhook_key"`
		Nodes          []workflowNode `json:"nodes"`
		Edges          []workflowEdge `json:"edges"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, body.OrganizationID, actAdmin) {
		return
	}
	tx, err := rs.DB.Beginx()
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer tx.Rollback()

	var id int64
	if err := tx.QueryRowxContext(r.Context(),
		`INSERT INTO workflow_templates (organization_id, name, webhook_enabled, webhook_service, webhook_key)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		body.OrganizationID, body.Name, body.WebhookEnabled, nullIfEmpty(body.WebhookService), nullIfEmpty(body.WebhookKey)).Scan(&id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	for _, n := range body.Nodes {
		if n.NodeType == "" {
			n.NodeType = "job"
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO workflow_nodes (workflow_template_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, n.NodeKey, n.NodeType, n.JobTemplateID, n.Name, nullIfEmpty(n.WebhookURL), nullIfEmpty(n.WebhookBody)); err != nil {
			render.ErrInvalidRequest(err).Render(w, r)
			return
		}
	}
	for _, e := range body.Edges {
		if e.EdgeType == "" {
			e.EdgeType = "success"
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO workflow_node_edges (workflow_template_id, parent_key, child_key, edge_type)
			 VALUES ($1, $2, $3, $4)`, id, e.ParentKey, e.ChildKey, e.EdgeType); err != nil {
			render.ErrInvalidRequest(err).Render(w, r)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"id": id})
}

// UpdateWorkflow PUT /api/v1/workflow-templates/{id} — edit a template's name,
// webhook trigger and its whole node/edge graph (replaced wholesale). webhook_key
// is preserved unless a new non-empty value is supplied (it's never returned). In
// -flight runs snapshot their nodes at launch but read edges live, so prefer
// editing when the workflow has no active run.
func (rs *WorkflowsResource) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	org, ok := rs.orgOfWorkflow(r, id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}
	var body struct {
		Name           string         `json:"name"`
		WebhookEnabled bool           `json:"webhook_enabled"`
		WebhookService string         `json:"webhook_service"`
		WebhookKey     string         `json:"webhook_key"`
		Nodes          []workflowNode `json:"nodes"`
		Edges          []workflowEdge `json:"edges"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	tx, err := rs.DB.Beginx()
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(),
		`UPDATE workflow_templates SET name=$2, webhook_enabled=$3, webhook_service=$4,
		        webhook_key=COALESCE(NULLIF($5,''), webhook_key), modified_at=now()
		 WHERE id=$1`,
		id, body.Name, body.WebhookEnabled, nullIfEmpty(body.WebhookService), nullIfEmpty(body.WebhookKey)); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	// Replace the graph wholesale.
	tx.ExecContext(r.Context(), `DELETE FROM workflow_node_edges WHERE workflow_template_id=$1`, id)
	tx.ExecContext(r.Context(), `DELETE FROM workflow_nodes WHERE workflow_template_id=$1`, id)
	for _, n := range body.Nodes {
		if n.NodeType == "" {
			n.NodeType = "job"
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO workflow_nodes (workflow_template_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, n.NodeKey, n.NodeType, n.JobTemplateID, n.Name, nullIfEmpty(n.WebhookURL), nullIfEmpty(n.WebhookBody)); err != nil {
			render.ErrInvalidRequest(err).Render(w, r)
			return
		}
	}
	for _, e := range body.Edges {
		if e.EdgeType == "" {
			e.EdgeType = "success"
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO workflow_node_edges (workflow_template_id, parent_key, child_key, edge_type)
			 VALUES ($1, $2, $3, $4)`, id, e.ParentKey, e.ChildKey, e.EdgeType); err != nil {
			render.ErrInvalidRequest(err).Render(w, r)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, map[string]interface{}{"id": id})
}

// GetWorkflow GET /api/v1/workflow-templates/{id}
func (rs *WorkflowsResource) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	org, ok := rs.orgOfWorkflow(r, id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actRead) {
		return
	}
	nodes := []workflowNode{}
	_ = rs.DB.SelectContext(r.Context(), &nodes,
		`SELECT node_key, node_type, job_template_id, name,
		        COALESCE(webhook_url,'') AS webhook_url, COALESCE(webhook_body,'') AS webhook_body
		 FROM workflow_nodes WHERE workflow_template_id=$1`, id)
	edges := []workflowEdge{}
	_ = rs.DB.SelectContext(r.Context(), &edges,
		`SELECT parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$1`, id)
	var wh struct {
		Enabled bool   `db:"webhook_enabled"`
		Service string `db:"webhook_service"`
	}
	_ = rs.DB.GetContext(r.Context(), &wh,
		`SELECT webhook_enabled, COALESCE(webhook_service,'') AS webhook_service FROM workflow_templates WHERE id=$1`, id)
	render.JSON(w, r, map[string]interface{}{
		"id": id, "organization_id": org, "nodes": nodes, "edges": edges,
		"webhook_enabled": wh.Enabled, "webhook_service": wh.Service,
	})
}

// DeleteWorkflow DELETE /api/v1/workflow-templates/{id}
func (rs *WorkflowsResource) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	org, ok := rs.orgOfWorkflow(r, id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}
	if _, err := rs.DB.ExecContext(r.Context(), `DELETE FROM workflow_templates WHERE id=$1`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LaunchWorkflow POST /api/v1/workflow-templates/{id}/launch — snapshot nodes into
// a workflow_jobs run that the scheduler's workflow runner then walks.
func (rs *WorkflowsResource) LaunchWorkflow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	org, ok := rs.orgOfWorkflow(r, id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}
	tx, err := rs.DB.Beginx()
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer tx.Rollback()

	var wjID int64
	if err := tx.QueryRowxContext(r.Context(),
		`INSERT INTO workflow_jobs (workflow_template_id, status) VALUES ($1, 'running') RETURNING id`, id).Scan(&wjID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`INSERT INTO workflow_job_nodes (workflow_job_id, node_key, node_type, job_template_id, status)
		 SELECT $1, node_key, node_type, job_template_id, 'pending' FROM workflow_nodes WHERE workflow_template_id=$2`,
		wjID, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if err := tx.Commit(); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"workflow_job_id": wjID, "status": "running"})
}

// ListWorkflowJobs GET /api/v1/workflow-jobs — recent runs the user can see.
func (rs *WorkflowsResource) ListWorkflowJobs(w http.ResponseWriter, r *http.Request) {
	type run struct {
		ID                 int64      `json:"id" db:"id"`
		WorkflowTemplateID int64      `json:"workflow_template_id" db:"workflow_template_id"`
		TemplateName       string     `json:"template_name" db:"template_name"`
		OrganizationID     int64      `json:"organization_id" db:"organization_id"`
		Status             string     `json:"status" db:"status"`
		CreatedAt          time.Time  `json:"created_at" db:"created_at"`
		FinishedAt         *time.Time `json:"finished_at" db:"finished_at"`
	}
	rows := []run{}
	orgIDs, err := rs.readableIDs(r, rbac.ContentTypeOrganization)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if len(orgIDs) > 0 {
		q, args, _ := sqlx.In(`
			SELECT wj.id, wj.workflow_template_id, wt.name AS template_name,
			       wt.organization_id, wj.status, wj.created_at, wj.finished_at
			FROM workflow_jobs wj
			JOIN workflow_templates wt ON wt.id = wj.workflow_template_id
			WHERE wt.organization_id IN (?)
			ORDER BY wj.id DESC LIMIT 100`, orgIDs)
		q = rs.DB.Rebind(q)
		if err := rs.DB.SelectContext(r.Context(), &rows, q, args...); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
	}
	render.JSON(w, r, rows)
}

// GetWorkflowJob GET /api/v1/workflow-jobs/{id} — full run detail: status, the
// template's structure (node names + edges) and each node's live status, so a
// run page can draw and refresh the DAG on its own.
func (rs *WorkflowsResource) GetWorkflowJob(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var meta struct {
		Org        int64      `db:"organization_id"`
		TemplateID int64      `db:"workflow_template_id"`
		Name       string     `db:"name"`
		Status     string     `db:"status"`
		CreatedAt  time.Time  `db:"created_at"`
		FinishedAt *time.Time `db:"finished_at"`
	}
	if err := rs.DB.GetContext(r.Context(), &meta, `
		SELECT wt.organization_id, wj.workflow_template_id, wt.name, wj.status, wj.created_at, wj.finished_at
		FROM workflow_jobs wj
		JOIN workflow_templates wt ON wt.id = wj.workflow_template_id
		WHERE wj.id=$1`, id); err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, meta.Org, actRead) {
		return
	}
	type node struct {
		ID           int64   `json:"id" db:"id"`
		NodeKey      string  `json:"node_key" db:"node_key"`
		NodeType     string  `json:"node_type" db:"node_type"`
		Name         string  `json:"name" db:"name"`
		UnifiedJobID *int64  `json:"unified_job_id" db:"unified_job_id"`
		RunID        *string `json:"run_id" db:"run_id"`
		Status       string  `json:"status" db:"status"`
		EventToken   string  `json:"-" db:"event_token"`
		// CallbackURL is populated only while a webhook_in node is awaiting_event,
		// so an operator can wire the external system that releases it.
		CallbackURL string `json:"callback_url,omitempty" db:"-"`
	}
	nodes := []node{}
	// run_id is the node's latest execution run, so the UI can show the engine
	// lifecycle (agentless bootstrap, checkpoints, resume) per workflow step.
	_ = rs.DB.SelectContext(r.Context(), &nodes, `
		SELECT wjn.id, wjn.node_key, wjn.node_type,
		       COALESCE(wn.name, '') AS name, wjn.unified_job_id, wjn.status,
		       COALESCE(wjn.event_token, '') AS event_token,
		       er.id AS run_id
		FROM workflow_job_nodes wjn
		LEFT JOIN workflow_nodes wn
		       ON wn.workflow_template_id = $1 AND wn.node_key = wjn.node_key
		LEFT JOIN LATERAL (
		       SELECT id FROM execution_runs
		       WHERE unified_job_id = wjn.unified_job_id
		       ORDER BY created_at DESC LIMIT 1
		) er ON true
		WHERE wjn.workflow_job_id = $2
		ORDER BY wjn.id`, meta.TemplateID, id)
	for i := range nodes {
		if nodes[i].Status == "awaiting_event" && nodes[i].EventToken != "" {
			nodes[i].CallbackURL = fmt.Sprintf(
				"/api/v1/webhooks/workflow-job-nodes/%d/callback?token=%s", nodes[i].ID, nodes[i].EventToken)
		}
	}
	edges := []workflowEdge{}
	_ = rs.DB.SelectContext(r.Context(), &edges,
		`SELECT parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$1`, meta.TemplateID)
	render.JSON(w, r, map[string]interface{}{
		"id": id, "workflow_template_id": meta.TemplateID, "name": meta.Name,
		"status": meta.Status, "created_at": meta.CreatedAt, "finished_at": meta.FinishedAt,
		"nodes": nodes, "edges": edges,
	})
}

func (rs *WorkflowsResource) setNodeApproval(w http.ResponseWriter, r *http.Request, status string) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// Gate on the owning workflow's org.
	var org int64
	if err := rs.DB.GetContext(r.Context(), &org, `
		SELECT wt.organization_id
		FROM workflow_job_nodes wjn
		JOIN workflow_jobs wj ON wj.id = wjn.workflow_job_id
		JOIN workflow_templates wt ON wt.id = wj.workflow_template_id
		WHERE wjn.id=$1`, id); err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}
	if _, err := rs.DB.ExecContext(r.Context(),
		`UPDATE workflow_job_nodes SET status=$1 WHERE id=$2 AND status='awaiting_approval'`, status, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ApproveNode POST /api/v1/workflow-job-nodes/{id}/approve
func (rs *WorkflowsResource) ApproveNode(w http.ResponseWriter, r *http.Request) {
	rs.setNodeApproval(w, r, "approved")
}

// DenyNode POST /api/v1/workflow-job-nodes/{id}/deny
func (rs *WorkflowsResource) DenyNode(w http.ResponseWriter, r *http.Request) {
	rs.setNodeApproval(w, r, "rejected")
}
