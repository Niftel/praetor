package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

// TestWorkflowRBAC proves workflow templates are first-class RBAC objects: the
// create-time trigger (migration 000049) gives each workflow its own
// admin/execute/approval/read roles, and the handlers gate on those per-object
// roles rather than the owning org.
func TestWorkflowRBAC(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	wf := handlers.NewWorkflowsResource(db, handlers.NewAuthorizer(db))
	access := rbac.NewStore(db, testResourceTables)

	uniq := time.Now().UnixNano()
	org := createOrg(t, db, fmt.Sprintf("wf-rbac-org-%d", uniq))
	creator := createUser(t, db, fmt.Sprintf("wf-creator-%d", uniq))    // org workflow_admin
	perWfAdmin := createUser(t, db, fmt.Sprintf("wf-wfadmin-%d", uniq)) // only wfA admin_role
	execOnly := createUser(t, db, fmt.Sprintf("wf-exec-%d", uniq))      // only wfA execute_role
	orgExec := createUser(t, db, fmt.Sprintf("wf-orgexec-%d", uniq))    // org execute_role
	approver := createUser(t, db, fmt.Sprintf("wf-approver-%d", uniq))  // only wfA approval_role
	nobody := createUser(t, db, fmt.Sprintf("wf-nobody-%d", uniq))
	otherTeamUser := createUser(t, db, fmt.Sprintf("wf-other-team-%d", uniq))
	grantObjectRole(t, access, rbac.Organization, org, rbac.WorkflowAdminRole, creator)
	grantObjectRole(t, access, rbac.Organization, org, rbac.ExecuteRole, orgExec)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id = $1`, org)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5,$6,$7)`, creator, perWfAdmin, execOnly, orgExec, approver, nobody, otherTeamUser)
	})

	creatorUC := middleware.UserContext{UserID: creator}
	body := func(name string) string {
		return fmt.Sprintf(`{"organization_id":%d,"name":%q}`, org, name)
	}
	updateBody := func(name string) string {
		return fmt.Sprintf(`{"name":%q,"nodes":[{"node_key":"team-gate","node_type":"approval","name":"Team gate"}]}`, name)
	}

	// 1. Org workflow_admin creates two workflows -> 201; creator becomes admin.
	rec := callJSON(t, wf.CreateWorkflow, http.MethodPost, body(fmt.Sprintf("wfA-%d", uniq)), creatorUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create wfA: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	wfA := extractID(t, rec.Body.String())
	if _, err := db.Exec(`INSERT INTO workflow_nodes
		(workflow_template_id,node_key,node_type,name,approval_timeout_seconds,approval_timeout_action)
		VALUES ($1,'team-gate','approval','Team gate',86400,'rejected')`, wfA); err != nil {
		t.Fatalf("add approval node: %v", err)
	}
	var approvalTeam, otherTeam int64
	if err := db.Get(&approvalTeam, `INSERT INTO teams (organization_id,name) VALUES ($1,$2) RETURNING id`, org, fmt.Sprintf("approvers-%d", uniq)); err != nil {
		t.Fatalf("create approval team: %v", err)
	}
	if err := db.Get(&otherTeam, `INSERT INTO teams (organization_id,name) VALUES ($1,$2) RETURNING id`, org, fmt.Sprintf("other-%d", uniq)); err != nil {
		t.Fatalf("create other team: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO team_members (team_id,user_id) VALUES ($1,$2),($1,$3),($4,$5)`, approvalTeam, execOnly, approver, otherTeam, otherTeamUser); err != nil {
		t.Fatalf("seed team members: %v", err)
	}
	rec = callJSON(t, wf.CreateWorkflow, http.MethodPost, body(fmt.Sprintf("wfB-%d", uniq)), creatorUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create wfB: want 201, got %d", rec.Code)
	}
	wfB := extractID(t, rec.Body.String())

	if ok, err := capCheck(access, creator, rbac.WorkflowTemplate, wfA, rbac.Manage); err != nil || !ok {
		t.Fatalf("creator should administer wfA (creator-admin grant): ok=%v err=%v", ok, err)
	}

	// 2. Per-workflow admin (only wfA.admin_role) edits wfA but not wfB.
	grantObjectRole(t, access, rbac.WorkflowTemplate, wfA, rbac.AdminRole, perWfAdmin)
	pwaUC := middleware.UserContext{UserID: perWfAdmin}
	rec = callJSON(t, wf.UpdateWorkflow, http.MethodPut, updateBody("wfA-edited"), pwaUC, map[string]string{"id": fmt.Sprint(wfA)})
	if rec.Code != http.StatusOK {
		t.Fatalf("per-wf admin edit own: want 200, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, wf.UpdateWorkflow, http.MethodPut, updateBody("wfB-edited"), pwaUC, map[string]string{"id": fmt.Sprint(wfB)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("per-wf admin edit sibling: want 403, got %d", rec.Code)
	}

	// 3. Execute-only: launch authz passes (not 403) + read, but cannot edit.
	grantObjectRole(t, access, rbac.WorkflowTemplate, wfA, rbac.ExecuteRole, execOnly)
	if ok, err := capCheck(access, execOnly, rbac.WorkflowTemplate, wfA, rbac.Execute); err != nil || !ok {
		t.Fatalf("execute-only should have execute on wfA: ok=%v err=%v", ok, err)
	}
	if ok, _ := capCheck(access, execOnly, rbac.WorkflowTemplate, wfA, rbac.View); !ok {
		t.Fatalf("execute-only should read wfA (read is a child of execute)")
	}
	if ok, _ := capCheck(access, execOnly, rbac.WorkflowTemplate, wfA, rbac.Manage); ok {
		t.Fatalf("execute-only must NOT admin wfA")
	}
	execUC := middleware.UserContext{UserID: execOnly}
	rec = callJSON(t, wf.UpdateWorkflow, http.MethodPut, updateBody("nope"), execUC, map[string]string{"id": fmt.Sprint(wfA)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("execute-only edit: want 403, got %d", rec.Code)
	}
	rec = callJSON(t, wf.LaunchWorkflow, http.MethodPost, `{"extra_vars":{"release":"canary"},"limit":"web-*"}`, execUC, map[string]string{"id": fmt.Sprint(wfA)})
	if rec.Code == http.StatusForbidden {
		t.Fatalf("execute-only launch: authz gate should pass, got 403")
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("execute-only launch: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	var launched struct {
		WorkflowJobID int64 `json:"workflow_job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &launched); err != nil {
		t.Fatalf("decode workflow launch: %v", err)
	}
	var launchArgs []byte
	if err := db.Get(&launchArgs, `SELECT launch_args FROM workflow_jobs WHERE id=$1`, launched.WorkflowJobID); err != nil {
		t.Fatalf("read workflow launch args: %v", err)
	}
	var args struct {
		ExtraVars map[string]interface{} `json:"extra_vars"`
		Limit     *string                `json:"limit"`
	}
	if err := json.Unmarshal(launchArgs, &args); err != nil {
		t.Fatalf("decode workflow launch args: %v", err)
	}
	if args.ExtraVars["release"] != "canary" || args.Limit == nil || *args.Limit != "web-*" {
		t.Fatalf("workflow launch inputs were not preserved: %s", launchArgs)
	}
	var launchedBy int64
	if err := db.Get(&launchedBy, `SELECT launched_by_user_id FROM workflow_jobs WHERE id=$1`, launched.WorkflowJobID); err != nil || launchedBy != execOnly {
		t.Fatalf("workflow requester was not recorded: got=%d err=%v", launchedBy, err)
	}
	var assignedTeam int64
	if err := db.Get(&assignedTeam, `SELECT approval_team_id FROM workflow_jobs WHERE id=$1`, launched.WorkflowJobID); err != nil || assignedTeam != approvalTeam {
		t.Fatalf("requester's only team should own approvals: got=%d want=%d err=%v", assignedTeam, approvalTeam, err)
	}

	// 4. The approval inbox contains only pending nodes on workflows the caller
	// may approve, and a second decision receives a stale-state conflict.
	grantObjectRole(t, access, rbac.WorkflowTemplate, wfA, rbac.ApprovalRole, approver)
	var approvalID int64
	if err := db.Get(&approvalID, `
		INSERT INTO workflow_job_nodes (workflow_job_id,node_key,node_type,name,status)
		VALUES ($1,'release-gate','approval','Release gate','awaiting_approval') RETURNING id`, launched.WorkflowJobID); err != nil {
		t.Fatalf("insert approval node: %v", err)
	}
	approverUC := middleware.UserContext{UserID: approver}
	rec = callJSON(t, wf.ListWorkflowApprovals, http.MethodGet, "", approverUC, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list approvals: want 200, got %d (%s)", rec.Code, rec.Body)
	}
	var approvals []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &approvals); err != nil || len(approvals) != 1 {
		t.Fatalf("approver should see one pending approval: err=%v body=%s", err, rec.Body)
	}
	if approvals[0]["requested_by"] != fmt.Sprintf("wf-exec-%d", uniq) || approvals[0]["awaiting_since"] == nil {
		t.Fatalf("approval audit context missing from inbox: %s", rec.Body)
	}
	if approvals[0]["approval_team"] != fmt.Sprintf("approvers-%d", uniq) {
		t.Fatalf("approval team missing from inbox: %s", rec.Body)
	}
	rec = callJSON(t, wf.ListWorkflowApprovals, http.MethodGet, "", execUC, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Fatalf("execute-only user must not see approvals: code=%d body=%s", rec.Code, rec.Body)
	}
	otherUC := middleware.UserContext{UserID: otherTeamUser}
	rec = callJSON(t, wf.ListWorkflowApprovals, http.MethodGet, "", otherUC, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Fatalf("another team must not see approvals: code=%d body=%s", rec.Code, rec.Body)
	}
	rec = callJSON(t, wf.ApproveNode, http.MethodPost, "", otherUC, map[string]string{"id": fmt.Sprint(approvalID)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("another team must not approve: want 403, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, wf.ApproveNode, http.MethodPost, "", execUC, map[string]string{"id": fmt.Sprint(approvalID)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("requester must not self-approve: want 403, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, wf.ApproveNode, http.MethodPost, "", approverUC, map[string]string{"id": fmt.Sprint(approvalID)})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("approve pending node: want 204, got %d (%s)", rec.Code, rec.Body)
	}
	var decision struct {
		DecidedBy int64      `db:"decided_by_user_id"`
		DecidedAt *time.Time `db:"decided_at"`
	}
	if err := db.Get(&decision, `SELECT decided_by_user_id, decided_at FROM workflow_job_nodes WHERE id=$1`, approvalID); err != nil {
		t.Fatalf("read approval decision audit: %v", err)
	}
	if decision.DecidedBy != approver || decision.DecidedAt == nil {
		t.Fatalf("approval decision audit not recorded: %+v", decision)
	}
	rec = callJSON(t, wf.ApproveNode, http.MethodPost, "", approverUC, map[string]string{"id": fmt.Sprint(approvalID)})
	if rec.Code != http.StatusConflict {
		t.Fatalf("approve stale node: want 409, got %d (%s)", rec.Code, rec.Body)
	}

	// 5. Org execute_role holder can execute any workflow in the org (parent edge).
	if ok, err := capCheck(access, orgExec, rbac.WorkflowTemplate, wfB, rbac.Execute); err != nil || !ok {
		t.Fatalf("org-execute should run any org workflow (wfB): ok=%v err=%v", ok, err)
	}

	// 6. Approval is NOT inherited from the workflow admin_role.
	if ok, _ := capCheck(access, creator, rbac.WorkflowTemplate, wfA, rbac.Approve); ok {
		t.Fatalf("workflow admin must NOT inherit approval_role (manage != approve)")
	}

	// 7. List scoping: per-wf admin sees only wfA; a nobody sees none; the
	//    creator (org workflow_admin) sees both.
	if got := listWorkflowCount(t, wf, pwaUC); got != 1 {
		t.Fatalf("per-wf admin should see 1 workflow, saw %d", got)
	}
	if got := listWorkflowCount(t, wf, middleware.UserContext{UserID: nobody}); got != 0 {
		t.Fatalf("nobody should see 0 workflows, saw %d", got)
	}
	if got := listWorkflowCount(t, wf, creatorUC); got < 2 {
		t.Fatalf("org workflow_admin should see both workflows, saw %d", got)
	}

	// 8. Delete removes the workflow's capability object_roles (rbac_on_object_delete).
	rec = callJSON(t, wf.DeleteWorkflow, http.MethodDelete, "", creatorUC, map[string]string{"id": fmt.Sprint(wfB)})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete wfB: want 204, got %d", rec.Code)
	}
	var n int
	if err := db.Get(&n, `SELECT count(*) FROM object_roles WHERE content_type='workflow_template' AND object_id=$1`, wfB); err != nil {
		t.Fatalf("count object_roles: %v", err)
	}
	if n != 0 {
		t.Fatalf("deleted workflow should have no object_roles left, found %d", n)
	}
}

func listWorkflowCount(t *testing.T, wf *handlers.WorkflowsResource, uc middleware.UserContext) int {
	t.Helper()
	rec := callJSON(t, wf.ListWorkflows, http.MethodGet, "", uc, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list workflows: want 200, got %d", rec.Code)
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("parse workflow list: %v (%s)", err, rec.Body)
	}
	return len(arr)
}
