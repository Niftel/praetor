package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

// TestDelegatedOrgAdminRoles proves the org-level delegated admin roles are now
// live on create paths: a holder of org project_admin_role may create a project
// but not an inventory (that needs inventory_admin_role), and vice versa. Before
// this change every create checked the top-level org admin_role, so these
// delegated roles granted nothing on create. Org admins/superusers still pass
// because they inherit the sub-admin roles through the hierarchy.
func TestDelegatedOrgAdminRoles(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()

	h := handlers.NewProjectsResource(db)
	invRes := handlers.NewInventoriesResource(db)
	access := rbac.NewAccessChecker(db)

	uniq := time.Now().UnixNano()
	org := createOrg(t, db, fmt.Sprintf("rbac-deleg-org-%d", uniq))
	projAdmin := createUser(t, db, fmt.Sprintf("rbac-deleg-projadmin-%d", uniq))
	invAdmin := createUser(t, db, fmt.Sprintf("rbac-deleg-invadmin-%d", uniq))
	grantObjectRole(t, access, rbac.ContentTypeOrganization, org, rbac.RoleFieldProjectAdmin, projAdmin)
	grantObjectRole(t, access, rbac.ContentTypeOrganization, org, rbac.RoleFieldInventoryAdmin, invAdmin)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id = $1`, org)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, projAdmin, invAdmin)
	})

	projAdminUC := middleware.UserContext{UserID: projAdmin}
	invAdminUC := middleware.UserContext{UserID: invAdmin}

	invBody := fmt.Sprintf(`{"name":"inv-%d","organization_id":%d}`, uniq, org)

	// project_admin_role: CAN create a project, CANNOT create an inventory.
	rec := callJSON(t, h.CreateProject, http.MethodPost, projectBody(org, fmt.Sprintf("deleg-p-%d", uniq)), projAdminUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("project_admin create project: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, invRes.CreateInventory, http.MethodPost, invBody, projAdminUC, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("project_admin create inventory: want 403, got %d (%s)", rec.Code, rec.Body)
	}

	// inventory_admin_role: the mirror image.
	rec = callJSON(t, invRes.CreateInventory, http.MethodPost, invBody, invAdminUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("inventory_admin create inventory: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, h.CreateProject, http.MethodPost, projectBody(org, fmt.Sprintf("deleg-p2-%d", uniq)), invAdminUC, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("inventory_admin create project: want 403, got %d (%s)", rec.Code, rec.Body)
	}
}

// TestOrgExecuteRunsJobTemplates proves migration 000048's hierarchy fix: a
// holder of the org execute_role may execute (and read) any job template in the
// org, without any per-template grant. The org execute_role was previously inert
// because the trigger never made a JT's execute_role a child of it. Execute is
// still not admin — an org-execute holder cannot manage the template.
func TestOrgExecuteRunsJobTemplates(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	access := rbac.NewAccessChecker(db)
	ctx := context.Background()

	uniq := time.Now().UnixNano()
	org := createOrg(t, db, fmt.Sprintf("rbac-orgexec-org-%d", uniq))
	runner := createUser(t, db, fmt.Sprintf("rbac-orgexec-runner-%d", uniq)) // org execute_role
	nobody := createUser(t, db, fmt.Sprintf("rbac-orgexec-nobody-%d", uniq))

	// Insert a job template directly so the create_job_template_roles trigger
	// fires (bypassing handler-level project validation, which is orthogonal here).
	var ujtID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO unified_job_templates (name) VALUES ($1) RETURNING id`,
		fmt.Sprintf("ujt-%d", uniq)).Scan(&ujtID); err != nil {
		t.Fatalf("insert unified_job_template: %v", err)
	}
	var jtID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO job_templates (organization_id, name, playbook, unified_job_template_id)
		 VALUES ($1, $2, 'site.yml', $3) RETURNING id`,
		org, fmt.Sprintf("jt-%d", uniq), ujtID).Scan(&jtID); err != nil {
		t.Fatalf("insert job_template: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM job_templates WHERE id = $1`, jtID)
		_, _ = db.Exec(`DELETE FROM unified_job_templates WHERE id = $1`, ujtID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id = $1`, org)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, runner, nobody)
	})

	grantObjectRole(t, access, rbac.ContentTypeOrganization, org, rbac.RoleFieldExecute, runner)

	if ok, err := access.CanExecute(ctx, runner, rbac.ContentTypeJobTemplate, jtID); err != nil || !ok {
		t.Fatalf("org-execute holder should execute org JT: ok=%v err=%v", ok, err)
	}
	if ok, err := access.CanRead(ctx, runner, rbac.ContentTypeJobTemplate, jtID); err != nil || !ok {
		t.Fatalf("org-execute holder should read org JT: ok=%v err=%v", ok, err)
	}
	if ok, _ := access.CanAdmin(ctx, runner, rbac.ContentTypeJobTemplate, jtID); ok {
		t.Fatalf("org-execute holder must NOT administer the JT")
	}
	if ok, _ := access.CanExecute(ctx, nobody, rbac.ContentTypeJobTemplate, jtID); ok {
		t.Fatalf("unrelated user must not execute the JT")
	}
}

// TestUpdateRoleSyncsWithoutAdmin proves the AWX update_role now gates SCM sync:
// a holder of a project's update_role (but not admin) may trigger a sync, while
// a user with only read cannot. Previously SyncProject required admin_role, so
// update_role was inert.
func TestUpdateRoleSyncsWithoutAdmin(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()

	h := handlers.NewProjectsResource(db)
	access := rbac.NewAccessChecker(db)

	uniq := time.Now().UnixNano()
	org := createOrg(t, db, fmt.Sprintf("rbac-upd-org-%d", uniq))
	owner := createUser(t, db, fmt.Sprintf("rbac-upd-owner-%d", uniq))     // creates the project
	updater := createUser(t, db, fmt.Sprintf("rbac-upd-updater-%d", uniq)) // update_role only
	reader := createUser(t, db, fmt.Sprintf("rbac-upd-reader-%d", uniq))   // read_role only
	grantObjectRole(t, access, rbac.ContentTypeOrganization, org, rbac.RoleFieldAdmin, owner)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id = $1`, org)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, owner, updater, reader)
	})

	ownerUC := middleware.UserContext{UserID: owner}

	rec := callJSON(t, h.CreateProject, http.MethodPost, projectBody(org, fmt.Sprintf("upd-p-%d", uniq)), ownerUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner create project: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	projID := extractID(t, rec.Body.String())

	grantObjectRole(t, access, rbac.ContentTypeProject, projID, rbac.RoleFieldUpdate, updater)
	grantObjectRole(t, access, rbac.ContentTypeProject, projID, rbac.RoleFieldRead, reader)

	idParam := map[string]string{"id": fmt.Sprint(projID)}

	// update_role holder may sync (any non-forbidden outcome is a pass on the
	// authz gate; the sync itself may 4xx/5xx on SCM but must not be a 403).
	rec = callJSON(t, h.SyncProject, http.MethodPost, "", middleware.UserContext{UserID: updater}, idParam)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("update_role holder sync: got 403, want the authz gate to pass")
	}

	// read-only user is forbidden.
	rec = callJSON(t, h.SyncProject, http.MethodPost, "", middleware.UserContext{UserID: reader}, idParam)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read-only sync: want 403, got %d (%s)", rec.Code, rec.Body)
	}
}
