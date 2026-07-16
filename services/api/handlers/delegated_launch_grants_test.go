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

func TestDelegatedLaunchGrantAdministration(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	resource := handlers.NewServicePrincipalsResource(db, handlers.NewAuthorizer(db))
	access := rbac.NewStore(db, testResourceTables)

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("grant-org-%d", uniq))
	foreignOrgID := createOrg(t, db, fmt.Sprintf("grant-foreign-org-%d", uniq))
	adminID := createUser(t, db, fmt.Sprintf("grant-admin-%d", uniq))
	outsiderID := createUser(t, db, fmt.Sprintf("grant-outsider-%d", uniq))
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.AdminRole, adminID)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id IN ($1,$2)`, orgID, foreignOrgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, adminID, outsiderID)
	})

	var principalID, workflowID, inventoryID, foreignInventoryID, hostID, foreignHostID, groupID, teamID int64
	if err := db.Get(&principalID, `
		INSERT INTO service_principals (organization_id,name,created_by_user_id)
		VALUES ($1,$2,$3) RETURNING id`, orgID, fmt.Sprintf("grant-principal-%d", uniq), adminID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&workflowID, `
		INSERT INTO workflow_templates (organization_id,name) VALUES ($1,$2) RETURNING id`,
		orgID, fmt.Sprintf("grant-workflow-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&inventoryID, `
		INSERT INTO inventories (organization_id,name,kind) VALUES ($1,$2,'static') RETURNING id`,
		orgID, fmt.Sprintf("grant-inventory-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&foreignInventoryID, `
		INSERT INTO inventories (organization_id,name,kind) VALUES ($1,$2,'static') RETURNING id`,
		foreignOrgID, fmt.Sprintf("grant-foreign-inventory-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&hostID, `INSERT INTO hosts (inventory_id,name) VALUES ($1,$2) RETURNING id`,
		inventoryID, fmt.Sprintf("host-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&foreignHostID, `INSERT INTO hosts (inventory_id,name) VALUES ($1,$2) RETURNING id`,
		foreignInventoryID, fmt.Sprintf("foreign-host-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&groupID, `INSERT INTO groups (inventory_id,name) VALUES ($1,$2) RETURNING id`,
		inventoryID, fmt.Sprintf("group-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&teamID, `INSERT INTO teams (organization_id,name) VALUES ($1,$2) RETURNING id`,
		orgID, fmt.Sprintf("team-%d", uniq)); err != nil {
		t.Fatal(err)
	}

	admin := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: adminID}
	outsider := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: outsiderID}
	params := map[string]string{"id": fmt.Sprint(principalID)}
	expiry := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{
		"workflow_template_id":%d,
		"inventory_id":%d,
		"allowed_host_ids":[%d,%d],
		"allowed_group_ids":[%d,%d],
		"max_hosts":2,
		"allowed_extra_var_keys":["change_ticket","change_ticket","environment"],
		"approval_team_id":%d,
		"expires_at":%q
	}`, workflowID, inventoryID, hostID, hostID, groupID, groupID, teamID, expiry)

	rec := callJSON(t, resource.CreateGrant, http.MethodPost, body, outsider, params)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider create: want 403, got %d", rec.Code)
	}
	rec = callJSON(t, resource.CreateGrant, http.MethodPost, body, admin, params)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	var grant struct {
		ID                  int64    `json:"id"`
		AllowedHostIDs      []int64  `json:"allowed_host_ids"`
		AllowedGroupIDs     []int64  `json:"allowed_group_ids"`
		AllowedExtraVarKeys []string `json:"allowed_extra_var_keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &grant); err != nil || grant.ID == 0 {
		t.Fatalf("decode grant: %v (%s)", err, rec.Body)
	}
	if len(grant.AllowedHostIDs) != 1 || len(grant.AllowedGroupIDs) != 1 ||
		len(grant.AllowedExtraVarKeys) != 2 {
		t.Fatalf("grant scopes were not normalized: %+v", grant)
	}

	crossOrgBody := fmt.Sprintf(`{
		"workflow_template_id":%d,
		"inventory_id":%d,
		"expires_at":%q
	}`, workflowID, foreignInventoryID, expiry)
	rec = callJSON(t, resource.CreateGrant, http.MethodPost, crossOrgBody, admin, params)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-org inventory: want 400, got %d (%s)", rec.Code, rec.Body)
	}

	foreignHostBody := fmt.Sprintf(`{
		"workflow_template_id":%d,
		"inventory_id":%d,
		"allowed_host_ids":[%d],
		"expires_at":%q
	}`, workflowID, inventoryID, foreignHostID, expiry)
	rec = callJSON(t, resource.CreateGrant, http.MethodPost, foreignHostBody, admin, params)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign host: want 400, got %d (%s)", rec.Code, rec.Body)
	}

	invalidVarBody := fmt.Sprintf(`{
		"workflow_template_id":%d,
		"inventory_id":%d,
		"allowed_extra_var_keys":["valid","not-valid"],
		"expires_at":%q
	}`, workflowID, inventoryID, expiry)
	rec = callJSON(t, resource.CreateGrant, http.MethodPost, invalidVarBody, admin, params)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid variable name: want 400, got %d", rec.Code)
	}

	expiredBody := fmt.Sprintf(`{
		"workflow_template_id":%d,
		"inventory_id":%d,
		"not_before":"2026-01-01T00:00:00Z",
		"expires_at":"2026-01-02T00:00:00Z"
	}`, workflowID, inventoryID)
	rec = callJSON(t, resource.CreateGrant, http.MethodPost, expiredBody, admin, params)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expired grant: want 400, got %d", rec.Code)
	}

	grantParams := map[string]string{"id": fmt.Sprint(principalID), "grantID": fmt.Sprint(grant.ID)}
	rec = callJSON(t, resource.RevokeGrant, http.MethodDelete, "", admin, grantParams)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: want 204, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, resource.UpdateGrant, http.MethodPut, body, admin, grantParams)
	if rec.Code != http.StatusConflict {
		t.Fatalf("update revoked grant: want 409, got %d (%s)", rec.Code, rec.Body)
	}
}

func TestDelegatedLaunchGrantDatabaseFence(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	uniq := time.Now().UnixNano()
	orgA := createOrg(t, db, fmt.Sprintf("grant-fence-a-%d", uniq))
	orgB := createOrg(t, db, fmt.Sprintf("grant-fence-b-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("grant-fence-user-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id IN ($1,$2)`, orgA, orgB)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})
	var principalID, workflowID, inventoryID int64
	if err := db.Get(&principalID, `INSERT INTO service_principals
		(organization_id,name,created_by_user_id) VALUES ($1,$2,$3) RETURNING id`,
		orgA, fmt.Sprintf("fence-principal-%d", uniq), userID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&workflowID, `INSERT INTO workflow_templates
		(organization_id,name) VALUES ($1,$2) RETURNING id`,
		orgA, fmt.Sprintf("fence-workflow-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&inventoryID, `INSERT INTO inventories
		(organization_id,name,kind) VALUES ($1,$2,'static') RETURNING id`,
		orgB, fmt.Sprintf("fence-inventory-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO delegated_launch_grants
		    (organization_id,service_principal_id,workflow_template_id,inventory_id,expires_at)
		VALUES ($1,$2,$3,$4,now()+interval '1 hour')`,
		orgA, principalID, workflowID, inventoryID); err == nil {
		t.Fatal("database accepted cross-organization delegated grant")
	}
}
