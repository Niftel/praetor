package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/praetordev/rbac"
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
	access := rbac.NewAccessChecker(db)

	uniq := time.Now().UnixNano()
	org := createOrg(t, db, fmt.Sprintf("wf-rbac-org-%d", uniq))
	creator := createUser(t, db, fmt.Sprintf("wf-creator-%d", uniq))    // org workflow_admin
	perWfAdmin := createUser(t, db, fmt.Sprintf("wf-wfadmin-%d", uniq)) // only wfA admin_role
	execOnly := createUser(t, db, fmt.Sprintf("wf-exec-%d", uniq))      // only wfA execute_role
	orgExec := createUser(t, db, fmt.Sprintf("wf-orgexec-%d", uniq))    // org execute_role
	nobody := createUser(t, db, fmt.Sprintf("wf-nobody-%d", uniq))
	grantObjectRole(t, access, rbac.ContentTypeOrganization, org, rbac.RoleFieldWorkflowAdmin, creator)
	grantObjectRole(t, access, rbac.ContentTypeOrganization, org, rbac.RoleFieldExecute, orgExec)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id = $1`, org)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5)`, creator, perWfAdmin, execOnly, orgExec, nobody)
	})

	creatorUC := middleware.UserContext{UserID: creator}
	body := func(name string) string {
		return fmt.Sprintf(`{"organization_id":%d,"name":%q}`, org, name)
	}

	// 1. Org workflow_admin creates two workflows -> 201; creator becomes admin.
	rec := callJSON(t, wf.CreateWorkflow, http.MethodPost, body(fmt.Sprintf("wfA-%d", uniq)), creatorUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create wfA: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	wfA := extractID(t, rec.Body.String())
	rec = callJSON(t, wf.CreateWorkflow, http.MethodPost, body(fmt.Sprintf("wfB-%d", uniq)), creatorUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create wfB: want 201, got %d", rec.Code)
	}
	wfB := extractID(t, rec.Body.String())

	if ok, err := capCheck(access, creator, rbac.ContentTypeWorkflowTemplate, wfA, rbac.ActionManage); err != nil || !ok {
		t.Fatalf("creator should administer wfA (creator-admin grant): ok=%v err=%v", ok, err)
	}

	// 2. Per-workflow admin (only wfA.admin_role) edits wfA but not wfB.
	grantObjectRole(t, access, rbac.ContentTypeWorkflowTemplate, wfA, rbac.RoleFieldAdmin, perWfAdmin)
	pwaUC := middleware.UserContext{UserID: perWfAdmin}
	rec = callJSON(t, wf.UpdateWorkflow, http.MethodPut, body("wfA-edited"), pwaUC, map[string]string{"id": fmt.Sprint(wfA)})
	if rec.Code != http.StatusOK {
		t.Fatalf("per-wf admin edit own: want 200, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, wf.UpdateWorkflow, http.MethodPut, body("wfB-edited"), pwaUC, map[string]string{"id": fmt.Sprint(wfB)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("per-wf admin edit sibling: want 403, got %d", rec.Code)
	}

	// 3. Execute-only: launch authz passes (not 403) + read, but cannot edit.
	grantObjectRole(t, access, rbac.ContentTypeWorkflowTemplate, wfA, rbac.RoleFieldExecute, execOnly)
	if ok, err := capCheck(access, execOnly, rbac.ContentTypeWorkflowTemplate, wfA, rbac.ActionExecute); err != nil || !ok {
		t.Fatalf("execute-only should have execute on wfA: ok=%v err=%v", ok, err)
	}
	if ok, _ := capCheck(access, execOnly, rbac.ContentTypeWorkflowTemplate, wfA, rbac.ActionView); !ok {
		t.Fatalf("execute-only should read wfA (read is a child of execute)")
	}
	if ok, _ := capCheck(access, execOnly, rbac.ContentTypeWorkflowTemplate, wfA, rbac.ActionManage); ok {
		t.Fatalf("execute-only must NOT admin wfA")
	}
	execUC := middleware.UserContext{UserID: execOnly}
	rec = callJSON(t, wf.UpdateWorkflow, http.MethodPut, body("nope"), execUC, map[string]string{"id": fmt.Sprint(wfA)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("execute-only edit: want 403, got %d", rec.Code)
	}
	rec = callJSON(t, wf.LaunchWorkflow, http.MethodPost, "", execUC, map[string]string{"id": fmt.Sprint(wfA)})
	if rec.Code == http.StatusForbidden {
		t.Fatalf("execute-only launch: authz gate should pass, got 403")
	}

	// 4. Org execute_role holder can execute any workflow in the org (parent edge).
	if ok, err := capCheck(access, orgExec, rbac.ContentTypeWorkflowTemplate, wfB, rbac.ActionExecute); err != nil || !ok {
		t.Fatalf("org-execute should run any org workflow (wfB): ok=%v err=%v", ok, err)
	}

	// 5. Approval is NOT inherited from the workflow admin_role.
	if ok, _ := capCheck(access, creator, rbac.ContentTypeWorkflowTemplate, wfA, rbac.ActionApprove); ok {
		t.Fatalf("workflow admin must NOT inherit approval_role (manage != approve)")
	}

	// 6. List scoping: per-wf admin sees only wfA; a nobody sees none; the
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

	// 7. Delete removes the workflow's capability object_roles (rbac_on_object_delete).
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
