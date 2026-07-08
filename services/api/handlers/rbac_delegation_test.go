package handlers_test

import (
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

	h := handlers.NewContentHandler(db)
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

// TestUpdateRoleSyncsWithoutAdmin proves the AWX update_role now gates SCM sync:
// a holder of a project's update_role (but not admin) may trigger a sync, while
// a user with only read cannot. Previously SyncProject required admin_role, so
// update_role was inert.
func TestUpdateRoleSyncsWithoutAdmin(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()

	h := handlers.NewContentHandler(db)
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
