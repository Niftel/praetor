package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/launch"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// WorkflowStore is the workflows-domain data access the handler depends on.
type WorkflowStore interface {
	OrgOf(ctx context.Context, id int64) (int64, bool)
	ListByIDs(ctx context.Context, ids []int64) ([]store.WorkflowSummary, error)
	Create(ctx context.Context, spec store.WorkflowSpec) (int64, error)
	Update(ctx context.Context, id int64, spec store.WorkflowSpec) error
	TemplateNodes(ctx context.Context, templateID int64) ([]store.WorkflowNode, error)
	TemplateEdges(ctx context.Context, templateID int64) ([]store.WorkflowEdge, error)
	TemplateMeta(ctx context.Context, templateID int64) (store.WorkflowMeta, error)
	Delete(ctx context.Context, id int64) error
	AllowSimultaneous(ctx context.Context, id int64) bool
	ActiveRunCount(ctx context.Context, id int64) (int, error)
	LaunchSnapshot(ctx context.Context, templateID int64, opts launch.Options) (int64, error)
	ListJobsByTemplates(ctx context.Context, templateIDs []int64) ([]store.WorkflowRun, error)
	JobMeta(ctx context.Context, id int64) (store.WorkflowJobMeta, error)
	JobNodes(ctx context.Context, jobID int64) ([]store.WorkflowJobNode, error)
	JobEdges(ctx context.Context, jobID int64) ([]store.WorkflowEdge, error)
	NodeApprovalTemplate(ctx context.Context, nodeID int64) (int64, error)
	SetNodeApproval(ctx context.Context, nodeID int64, status string) error
}

type WorkflowsResource struct {
	DB *sqlx.DB
	*Authorizer
	store         WorkflowStore
	notifications NotificationStore
}

func NewWorkflowsResource(db *sqlx.DB) *WorkflowsResource {
	return &WorkflowsResource{DB: db, Authorizer: NewAuthorizer(db), store: store.NewWorkflowStore(db), notifications: store.NewNotificationStore(db)}
}

// workflowNode / workflowEdge alias the store DTOs so handler code reads unchanged.
type workflowNode = store.WorkflowNode
type workflowEdge = store.WorkflowEdge

// ListWorkflows GET /api/v1/workflow-templates
func (rs *WorkflowsResource) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	// Object-role model: a user sees only the workflows they can read.
	// Superusers/auditors get everything via readableIDs.
	ids, err := rs.readableIDs(r, rbac.ContentTypeWorkflowTemplate)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	rows, err := rs.store.ListByIDs(r.Context(), ids)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}

// CreateWorkflow POST /api/v1/workflow-templates
func (rs *WorkflowsResource) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrganizationID    int64          `json:"organization_id"`
		Name              string         `json:"name"`
		WebhookEnabled    bool           `json:"webhook_enabled"`
		WebhookService    string         `json:"webhook_service"`
		WebhookKey        string         `json:"webhook_key"`
		AllowSimultaneous bool           `json:"allow_simultaneous"`
		Nodes             []workflowNode `json:"nodes"`
		Edges             []workflowEdge `json:"edges"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorizeOrgRole(w, r, body.OrganizationID, rbac.RoleFieldWorkflowAdmin) {
		return
	}
	id, err := rs.store.Create(r.Context(), store.WorkflowSpec{
		OrganizationID:    body.OrganizationID,
		Name:              body.Name,
		WebhookEnabled:    body.WebhookEnabled,
		WebhookService:    body.WebhookService,
		WebhookKey:        body.WebhookKey,
		AllowSimultaneous: body.AllowSimultaneous,
		Nodes:             body.Nodes,
		Edges:             body.Edges,
	})
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	// Creator becomes admin of the new workflow (AWX creator-admin), matching
	// job templates — so a non-superuser can manage what they create.
	rs.grantCreatorAdmin(r.Context(), rbac.ContentTypeWorkflowTemplate, id, currentUser(r))
	render.Created(w, r, map[string]interface{}{"id": id})
}

// UpdateWorkflow PUT /api/v1/workflow-templates/{id} — edit a template's name,
// webhook trigger and its whole node/edge graph (replaced wholesale). webhook_key
// is preserved unless a new non-empty value is supplied (it's never returned). In
// -flight runs snapshot their nodes at launch but read edges live, so prefer
// editing when the workflow has no active run.
func (rs *WorkflowsResource) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, ok := rs.store.OrgOf(r.Context(), id); !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeWorkflowTemplate, id, actAdmin) {
		return
	}
	var body struct {
		Name              string         `json:"name"`
		WebhookEnabled    bool           `json:"webhook_enabled"`
		WebhookService    string         `json:"webhook_service"`
		WebhookKey        string         `json:"webhook_key"`
		AllowSimultaneous bool           `json:"allow_simultaneous"`
		Nodes             []workflowNode `json:"nodes"`
		Edges             []workflowEdge `json:"edges"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if err := rs.store.Update(r.Context(), id, store.WorkflowSpec{
		Name:              body.Name,
		WebhookEnabled:    body.WebhookEnabled,
		WebhookService:    body.WebhookService,
		WebhookKey:        body.WebhookKey,
		AllowSimultaneous: body.AllowSimultaneous,
		Nodes:             body.Nodes,
		Edges:             body.Edges,
	}); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, map[string]interface{}{"id": id})
}

// GetWorkflow GET /api/v1/workflow-templates/{id}
func (rs *WorkflowsResource) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	org, ok := rs.store.OrgOf(r.Context(), id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeWorkflowTemplate, id, actRead) {
		return
	}
	nodes, _ := rs.store.TemplateNodes(r.Context(), id)
	edges, _ := rs.store.TemplateEdges(r.Context(), id)
	wh, _ := rs.store.TemplateMeta(r.Context(), id)
	render.JSON(w, r, map[string]interface{}{
		"id": id, "organization_id": org, "nodes": nodes, "edges": edges,
		"webhook_enabled": wh.Enabled, "webhook_service": wh.Service,
		"allow_simultaneous": wh.AllowSim,
	})
}

// DeleteWorkflow DELETE /api/v1/workflow-templates/{id}
func (rs *WorkflowsResource) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, ok := rs.store.OrgOf(r.Context(), id); !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeWorkflowTemplate, id, actAdmin) {
		return
	}
	if err := rs.store.Delete(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LaunchWorkflow POST /api/v1/workflow-templates/{id}/launch — snapshot nodes into
// a workflow_jobs run that the scheduler's workflow runner then walks.
func (rs *WorkflowsResource) LaunchWorkflow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, ok := rs.store.OrgOf(r.Context(), id); !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	// Launching a workflow is an execute action on the workflow. The org
	// execute_role is a parent of each workflow's execute_role (migration 000049),
	// so org-execute holders may run any workflow in the org.
	if !rs.authorize(w, r, rbac.ContentTypeWorkflowTemplate, id, actExecute) {
		return
	}

	// Concurrency guard: unless the workflow opts into simultaneous runs, refuse a
	// launch while a prior run is still active (prevents accidental double-triggers).
	if !rs.store.AllowSimultaneous(r.Context(), id) {
		if active, err := rs.store.ActiveRunCount(r.Context(), id); err == nil && active > 0 {
			render.ErrConflict(fmt.Errorf("this workflow is already running; wait for it to finish or enable Allow Simultaneous")).Render(w, r)
			return
		}
	}

	// Manual launches carry no overrides today: workflow-level prompt-on-launch
	// (extra_vars/survey/limit questions) is a separate feature. The typed Options
	// path exists so machine-originated launches (schedules, webhooks, EDA) can
	// carry context; wiring a manual prompt UI is now a one-place change (#90).
	wjID, err := rs.store.LaunchSnapshot(r.Context(), id, launch.Options{})
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"workflow_job_id": wjID, "status": "running"})
}

// ListWorkflowJobs GET /api/v1/workflow-jobs — recent runs the user can see.
func (rs *WorkflowsResource) ListWorkflowJobs(w http.ResponseWriter, r *http.Request) {
	ids, err := rs.readableIDs(r, rbac.ContentTypeWorkflowTemplate)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	rows, err := rs.store.ListJobsByTemplates(r.Context(), ids)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}

// GetWorkflowJob GET /api/v1/workflow-jobs/{id} — full run detail: status, the
// template's structure (node names + edges) and each node's live status, so a
// run page can draw and refresh the DAG on its own.
func (rs *WorkflowsResource) GetWorkflowJob(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	meta, err := rs.store.JobMeta(r.Context(), id)
	if err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeWorkflowTemplate, meta.TemplateID, actRead) {
		return
	}
	// run_id is the node's latest execution run, so the UI can show the engine
	// lifecycle (agentless bootstrap, checkpoints, resume) per workflow step.
	nodes, _ := rs.store.JobNodes(r.Context(), id)
	for i := range nodes {
		if nodes[i].Status == "awaiting_event" && nodes[i].EventToken != "" {
			nodes[i].CallbackURL = fmt.Sprintf(
				"/api/v1/webhooks/workflow-job-nodes/%d/callback?token=%s", nodes[i].ID, nodes[i].EventToken)
		}
	}
	edges, _ := rs.store.JobEdges(r.Context(), id)
	render.JSON(w, r, map[string]interface{}{
		"id": id, "workflow_template_id": meta.TemplateID, "name": meta.Name,
		"status": meta.Status, "created_at": meta.CreatedAt, "finished_at": meta.FinishedAt,
		"nodes": nodes, "edges": edges,
	})
}

func (rs *WorkflowsResource) setNodeApproval(w http.ResponseWriter, r *http.Request, status string) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// Gate on the workflow's own approval_role (parented to the org approval_role,
	// so org approvers still pass). A workflow_admin (manage) is deliberately NOT
	// an approver unless also granted approval_role — approving a gate is a
	// distinct authority from editing the workflow (AWX manage-vs-approve), and is
	// now delegable per-workflow.
	tplID, err := rs.store.NodeApprovalTemplate(r.Context(), id)
	if err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeWorkflowTemplate, tplID, actApprove) {
		return
	}
	if err := rs.store.SetNodeApproval(r.Context(), id, status); err != nil {
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
