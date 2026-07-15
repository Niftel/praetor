package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/launch"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
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

func NewWorkflowsResource(db *sqlx.DB, authz *Authorizer) *WorkflowsResource {
	return &WorkflowsResource{DB: db, Authorizer: authz, store: store.NewWorkflowStore(db), notifications: store.NewNotificationStore(db)}
}

// workflowNode / workflowEdge alias the store DTOs so handler code reads unchanged.
type workflowNode = store.WorkflowNode
type workflowEdge = store.WorkflowEdge

const approvalExpirySeconds = 24 * 60 * 60

// workflowNodeInput is the public create/update contract. Approval expiry is a
// server policy, deliberately absent here so clients cannot choose or override
// its duration or outcome.
type workflowNodeInput struct {
	NodeKey       string `json:"node_key"`
	NodeType      string `json:"node_type"`
	JobTemplateID *int64 `json:"job_template_id"`
	Name          string `json:"name"`
	WebhookURL    string `json:"webhook_url"`
	WebhookBody   string `json:"webhook_body"`
}

func workflowNodesFromInput(inputs []workflowNodeInput) []workflowNode {
	nodes := make([]workflowNode, len(inputs))
	for i, input := range inputs {
		nodes[i] = workflowNode{
			NodeKey:       input.NodeKey,
			NodeType:      input.NodeType,
			JobTemplateID: input.JobTemplateID,
			Name:          input.Name,
			WebhookURL:    input.WebhookURL,
			WebhookBody:   input.WebhookBody,
		}
	}
	return nodes
}

func decodeStrictJSON(r *http.Request, dst interface{}) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func validateWorkflowNodes(nodes []workflowNode) error {
	for i := range nodes {
		n := &nodes[i]
		n.ApprovalTimeoutSeconds = 0
		n.ApprovalTimeoutAction = "rejected"
		if n.NodeType == "approval" {
			n.ApprovalTimeoutSeconds = approvalExpirySeconds
		}
	}
	return nil
}

type workflowApproval struct {
	ID                 int64      `db:"id" json:"id"`
	WorkflowJobID      int64      `db:"workflow_job_id" json:"workflow_job_id"`
	WorkflowTemplateID int64      `db:"workflow_template_id" json:"workflow_template_id"`
	OrganizationID     int64      `db:"organization_id" json:"organization_id"`
	WorkflowName       string     `db:"workflow_name" json:"workflow_name"`
	NodeName           string     `db:"node_name" json:"node_name"`
	NodeKey            string     `db:"node_key" json:"node_key"`
	RunCreatedAt       time.Time  `db:"run_created_at" json:"run_created_at"`
	AwaitingSince      time.Time  `db:"awaiting_since" json:"awaiting_since"`
	RequestedBy        *string    `db:"requested_by" json:"requested_by,omitempty"`
	Deadline           *time.Time `db:"deadline" json:"deadline,omitempty"`
	TimeoutAction      string     `db:"timeout_action" json:"timeout_action"`
	ApprovalTeamID     *int64     `db:"approval_team_id" json:"approval_team_id,omitempty"`
	ApprovalTeam       *string    `db:"approval_team" json:"approval_team,omitempty"`
}

func (rs *WorkflowsResource) resolveApprovalTeam(ctx context.Context, workflowID, organizationID, userID, requestedTeamID int64, isSuperuser bool) (int64, error) {
	var hasApproval bool
	if err := rs.DB.GetContext(ctx, &hasApproval, `SELECT EXISTS (
		SELECT 1 FROM workflow_nodes WHERE workflow_template_id=$1 AND node_type='approval'
	)`, workflowID); err != nil {
		return 0, err
	}
	if !hasApproval && requestedTeamID == 0 {
		return 0, nil
	}

	teamIDs := []int64{}
	teamQuery := `SELECT t.id FROM teams t
		WHERE t.organization_id=$2 AND ($3 OR EXISTS (
			SELECT 1 FROM team_members tm WHERE tm.team_id=t.id AND tm.user_id=$1
		)) ORDER BY t.name, t.id`
	if err := rs.DB.SelectContext(ctx, &teamIDs, teamQuery, userID, organizationID, isSuperuser); err != nil {
		return 0, err
	}
	if requestedTeamID != 0 {
		for _, teamID := range teamIDs {
			if teamID == requestedTeamID {
				return teamID, nil
			}
		}
		return 0, fmt.Errorf("approval team must be one of your teams in this organization")
	}
	if len(teamIDs) == 0 {
		return 0, fmt.Errorf("an approval workflow can only be launched by a member of a team in this organization")
	}
	if len(teamIDs) > 1 {
		return 0, fmt.Errorf("approval_team_id is required because you belong to multiple teams in this organization")
	}
	return teamIDs[0], nil
}

// ListWorkflows GET /api/v1/workflow-templates
func (rs *WorkflowsResource) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	// Object-role model: a user sees only the workflows they can read.
	// Superusers/auditors get everything via readableIDs.
	ids, err := rs.readableIDs(r, rbac.WorkflowTemplate)
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
		OrganizationID    int64               `json:"organization_id"`
		Name              string              `json:"name"`
		WebhookEnabled    bool                `json:"webhook_enabled"`
		WebhookService    string              `json:"webhook_service"`
		WebhookKey        string              `json:"webhook_key"`
		AllowSimultaneous bool                `json:"allow_simultaneous"`
		Nodes             []workflowNodeInput `json:"nodes"`
		Edges             []workflowEdge      `json:"edges"`
	}
	if err := decodeStrictJSON(r, &body); err != nil || body.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	nodes := workflowNodesFromInput(body.Nodes)
	if err := validateWorkflowNodes(nodes); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorizeOrgRole(w, r, body.OrganizationID, rbac.WorkflowAdminRole) {
		return
	}
	id, err := rs.store.Create(r.Context(), store.WorkflowSpec{
		OrganizationID:    body.OrganizationID,
		Name:              body.Name,
		WebhookEnabled:    body.WebhookEnabled,
		WebhookService:    body.WebhookService,
		WebhookKey:        body.WebhookKey,
		AllowSimultaneous: body.AllowSimultaneous,
		Nodes:             nodes,
		Edges:             body.Edges,
	})
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	// Creator becomes admin of the new workflow (AWX creator-admin), matching
	// job templates — so a non-superuser can manage what they create.
	rs.grantCreatorAdmin(r.Context(), rbac.WorkflowTemplate, id, currentUser(r))
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
	if !rs.authorize(w, r, rbac.WorkflowTemplate, id, actAdmin) {
		return
	}
	var body struct {
		Name              string              `json:"name"`
		WebhookEnabled    bool                `json:"webhook_enabled"`
		WebhookService    string              `json:"webhook_service"`
		WebhookKey        string              `json:"webhook_key"`
		AllowSimultaneous bool                `json:"allow_simultaneous"`
		Nodes             []workflowNodeInput `json:"nodes"`
		Edges             []workflowEdge      `json:"edges"`
	}
	if err := decodeStrictJSON(r, &body); err != nil || body.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	nodes := workflowNodesFromInput(body.Nodes)
	if err := validateWorkflowNodes(nodes); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := rs.store.Update(r.Context(), id, store.WorkflowSpec{
		Name:              body.Name,
		WebhookEnabled:    body.WebhookEnabled,
		WebhookService:    body.WebhookService,
		WebhookKey:        body.WebhookKey,
		AllowSimultaneous: body.AllowSimultaneous,
		Nodes:             nodes,
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
	if !rs.authorize(w, r, rbac.WorkflowTemplate, id, actRead) {
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
	if !rs.authorize(w, r, rbac.WorkflowTemplate, id, actAdmin) {
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
	if !rs.authorize(w, r, rbac.WorkflowTemplate, id, actExecute) {
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

	// Manual launches use the same typed options path as schedules, webhooks and
	// EDA triggers. An empty body remains valid for existing clients and relaunch
	// actions, while operators can supply variables and a host limit when needed.
	var body struct {
		ExtraVars      map[string]interface{} `json:"extra_vars,omitempty"`
		Limit          *string                `json:"limit,omitempty"`
		ApprovalTeamID int64                  `json:"approval_team_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	organizationID, _ := rs.store.OrgOf(r.Context(), id)
	caller := currentUser(r)
	approvalTeamID, err := rs.resolveApprovalTeam(r.Context(), id, organizationID, caller.UserID, body.ApprovalTeamID, caller.IsSuperuser)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	tx, err := rs.DB.BeginTxx(r.Context(), nil)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer tx.Rollback()
	wjID, err := launch.Workflow(r.Context(), tx, id, launch.Options{
		ExtraVars: body.ExtraVars,
		Limit:     body.Limit,
	})
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE workflow_jobs SET launched_by_user_id=$1, approval_team_id=NULLIF($2, 0) WHERE id=$3`,
		currentUser(r).UserID, approvalTeamID, wjID); err != nil {
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
	ids, err := rs.readableIDs(r, rbac.WorkflowTemplate)
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

// ListWorkflowApprovals GET /api/v1/workflow-approvals — pending approval
// nodes assigned to the caller's team. System administrators retain a
// break-glass view, but requesters never see their own approval request.
func (rs *WorkflowsResource) ListWorkflowApprovals(w http.ResponseWriter, r *http.Request) {
	caller := currentUser(r)
	query := `
		SELECT wjn.id, wjn.workflow_job_id, wj.workflow_template_id,
		       wt.organization_id, wt.name AS workflow_name,
		       COALESCE(NULLIF(wjn.name, ''), wjn.node_key) AS node_name,
		       wjn.node_key, wj.created_at AS run_created_at,
		       COALESCE(wjn.awaiting_since, wj.created_at) AS awaiting_since,
		       launcher.username AS requested_by,
		       CASE WHEN wjn.approval_timeout_seconds > 0
		            THEN COALESCE(wjn.awaiting_since, wj.created_at) + make_interval(secs => wjn.approval_timeout_seconds)
		       END AS deadline,
		       wjn.approval_timeout_action AS timeout_action,
		       wj.approval_team_id, team.name AS approval_team
		FROM workflow_job_nodes wjn
		JOIN workflow_jobs wj ON wj.id = wjn.workflow_job_id
		JOIN workflow_templates wt ON wt.id = wj.workflow_template_id
		LEFT JOIN teams team ON team.id = wj.approval_team_id
		LEFT JOIN users launcher ON launcher.id = wj.launched_by_user_id
		WHERE wjn.status = 'awaiting_approval'
		  AND (wj.launched_by_user_id IS NULL OR wj.launched_by_user_id <> $2)
		  AND ($1 OR EXISTS (
		      SELECT 1 FROM team_members tm
		      WHERE tm.team_id=wj.approval_team_id AND tm.user_id=$2
		  ))
		ORDER BY wj.created_at ASC, wjn.id ASC`
	rows := []workflowApproval{}
	if err := rs.DB.SelectContext(r.Context(), &rows, query, caller.IsSuperuser, caller.UserID); err != nil {
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
	if !rs.authorize(w, r, rbac.WorkflowTemplate, meta.TemplateID, actRead) {
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
		"organization_id": func() int64 { orgID, _ := rs.store.OrgOf(r.Context(), meta.TemplateID); return orgID }(),
		"status":          meta.Status, "created_at": meta.CreatedAt, "finished_at": meta.FinishedAt,
		"nodes": nodes, "edges": edges,
	})
}

func (rs *WorkflowsResource) setNodeApproval(w http.ResponseWriter, r *http.Request, status string) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var assignment struct {
		TemplateID     int64  `db:"workflow_template_id"`
		RequesterID    *int64 `db:"launched_by_user_id"`
		ApprovalTeamID *int64 `db:"approval_team_id"`
	}
	err := rs.DB.GetContext(r.Context(), &assignment, `
		SELECT wj.workflow_template_id, wj.launched_by_user_id, wj.approval_team_id
		FROM workflow_job_nodes wjn
		JOIN workflow_jobs wj ON wj.id=wjn.workflow_job_id
		WHERE wjn.id=$1`, id)
	if err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	caller := currentUser(r)
	if assignment.RequesterID != nil && *assignment.RequesterID == caller.UserID {
		render.ErrForbidden(fmt.Errorf("requesters cannot decide their own approval")).Render(w, r)
		return
	}
	if !caller.IsSuperuser {
		if assignment.ApprovalTeamID == nil {
			render.ErrForbidden(fmt.Errorf("this approval has no assigned team")).Render(w, r)
			return
		}
		var member bool
		if err := rs.DB.GetContext(r.Context(), &member, `SELECT EXISTS (
			SELECT 1 FROM team_members WHERE team_id=$1 AND user_id=$2
		)`, *assignment.ApprovalTeamID, caller.UserID); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if !member {
			render.ErrForbidden(fmt.Errorf("this approval is assigned to another team")).Render(w, r)
			return
		}
	}
	if assignment.TemplateID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	result, err := rs.DB.ExecContext(r.Context(), `
		UPDATE workflow_job_nodes
		SET status=$1, decided_by_user_id=$2
		WHERE id=$3 AND status='awaiting_approval'`, status, currentUser(r).UserID, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if changed == 0 {
		render.ErrConflict(fmt.Errorf("this approval has already been decided")).Render(w, r)
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
