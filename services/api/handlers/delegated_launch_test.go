package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

func TestDelegatedWorkflowLaunchEnforcesScopeAndPersistsAttribution(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("delegated-launch-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("delegated-launch-user-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	var principalID, credentialID, workflowID, inventoryID, hostA, hostB, hostOutside, groupID, teamID, grantID int64
	mustGet := func(dest *int64, query string, args ...interface{}) {
		t.Helper()
		if err := db.Get(dest, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGet(&principalID, `INSERT INTO service_principals
		(organization_id,name,created_by_user_id) VALUES ($1,$2,$3) RETURNING id`,
		orgID, fmt.Sprintf("delegated-launch-principal-%d", uniq), userID)
	mustGet(&credentialID, `INSERT INTO service_credentials
		(service_principal_id,name,token_hash,expires_at,created_by_user_id)
		VALUES ($1,'test',$2,now()+interval '1 hour',$3) RETURNING id`,
		principalID, fmt.Sprintf("%064d", uniq), userID)
	mustGet(&workflowID, `INSERT INTO workflow_templates
		(organization_id,name,allow_simultaneous) VALUES ($1,$2,true) RETURNING id`,
		orgID, fmt.Sprintf("delegated-launch-workflow-%d", uniq))
	mustGet(&inventoryID, `INSERT INTO inventories
		(organization_id,name,kind) VALUES ($1,$2,'static') RETURNING id`,
		orgID, fmt.Sprintf("delegated-launch-inventory-%d", uniq))
	mustGet(&hostA, `INSERT INTO hosts (inventory_id,name) VALUES ($1,'app-02') RETURNING id`, inventoryID)
	mustGet(&hostB, `INSERT INTO hosts (inventory_id,name) VALUES ($1,'app-01') RETURNING id`, inventoryID)
	mustGet(&hostOutside, `INSERT INTO hosts (inventory_id,name) VALUES ($1,'db-01') RETURNING id`, inventoryID)
	mustGet(&groupID, `INSERT INTO groups (inventory_id,name) VALUES ($1,'apps') RETURNING id`, inventoryID)
	if _, err := db.Exec(`INSERT INTO host_groups (host_id,group_id) VALUES ($1,$3),($2,$3)`,
		hostA, hostB, groupID); err != nil {
		t.Fatal(err)
	}
	mustGet(&teamID, `INSERT INTO teams (organization_id,name) VALUES ($1,$2) RETURNING id`,
		orgID, fmt.Sprintf("delegated-launch-team-%d", uniq))
	mustGet(&grantID, `INSERT INTO delegated_launch_grants
		(organization_id,service_principal_id,workflow_template_id,inventory_id,
		 allowed_group_ids,max_hosts,allowed_extra_var_keys,approval_team_id,expires_at,
		 created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,2,$6,$7,now()+interval '1 hour',$8) RETURNING id`,
		orgID, principalID, workflowID, inventoryID, pq.Array([]int64{groupID}),
		pq.Array([]string{"change_ticket"}), teamID, userID)

	resource := handlers.NewDelegatedLaunchResource(db)
	principal := middleware.UserContext{
		Kind: middleware.ServicePrincipal, Username: "automation-client",
		ServicePrincipalID: principalID, ServiceCredentialID: credentialID,
		OrganizationID: orgID,
	}
	body := fmt.Sprintf(`{"external_requester":"customer-123","inventory_id":%d,
		"host_ids":[%d,%d],"extra_vars":{"change_ticket":"CHG-42"}}`,
		inventoryID, hostA, hostB)
	rec := callDelegatedLaunch(t, resource.LaunchWorkflow, workflowID, "request-1", body, principal)
	if rec.Code != http.StatusCreated {
		t.Fatalf("launch: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	var response struct {
		WorkflowJobID int64 `json:"workflow_job_id"`
		Replayed      bool  `json:"replayed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.WorkflowJobID == 0 || response.Replayed {
		t.Fatalf("unexpected launch response: %v (%s)", err, rec.Body)
	}

	var stored struct {
		PrincipalID      int64         `db:"launched_by_service_principal_id"`
		CredentialID     int64         `db:"launched_by_service_credential_id"`
		GrantID          int64         `db:"delegated_launch_grant_id"`
		External         string        `db:"delegated_external_requester"`
		InventoryID      int64         `db:"delegated_inventory_id"`
		HostIDs          pq.Int64Array `db:"delegated_host_ids"`
		ApprovalTeamID   int64         `db:"approval_team_id"`
		LaunchArgsString string        `db:"launch_args"`
	}
	if err := db.Get(&stored, `SELECT launched_by_service_principal_id,
		launched_by_service_credential_id,delegated_launch_grant_id,
		delegated_external_requester,delegated_inventory_id,delegated_host_ids,
		approval_team_id,launch_args::text AS launch_args
		FROM workflow_jobs WHERE id=$1`, response.WorkflowJobID); err != nil {
		t.Fatal(err)
	}
	if stored.PrincipalID != principalID || stored.CredentialID != credentialID ||
		stored.GrantID != grantID || stored.External != "customer-123" ||
		stored.InventoryID != inventoryID || stored.ApprovalTeamID != teamID ||
		len(stored.HostIDs) != 2 || !strings.Contains(stored.LaunchArgsString, `"limit": "app-01,app-02"`) {
		t.Fatalf("incorrect delegated attribution or launch args: %+v", stored)
	}

	var auditCount int
	if err := db.Get(&auditCount, `SELECT count(*) FROM activity_stream
		WHERE service_principal_id=$1 AND service_credential_id=$2
		  AND delegated_launch_grant_id=$3 AND external_requester='customer-123'
		  AND resource_type='workflow_template' AND resource_id=$4 AND action='launch'`,
		principalID, credentialID, grantID, workflowID); err != nil || auditCount != 1 {
		t.Fatalf("delegated audit count=%d err=%v", auditCount, err)
	}

	replay := callDelegatedLaunch(t, resource.LaunchWorkflow, workflowID, "request-1", body, principal)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay: want 200, got %d (%s)", replay.Code, replay.Body)
	}
	var replayResponse struct {
		WorkflowJobID int64 `json:"workflow_job_id"`
		Replayed      bool  `json:"replayed"`
	}
	_ = json.Unmarshal(replay.Body.Bytes(), &replayResponse)
	if !replayResponse.Replayed || replayResponse.WorkflowJobID != response.WorkflowJobID {
		t.Fatalf("unexpected replay response: %s", replay.Body)
	}

	conflictBody := strings.Replace(body, "customer-123", "customer-456", 1)
	if rec := callDelegatedLaunch(t, resource.LaunchWorkflow, workflowID, "request-1", conflictBody, principal); rec.Code != http.StatusConflict {
		t.Fatalf("changed idempotent request: want 409, got %d (%s)", rec.Code, rec.Body)
	}
	outsideBody := fmt.Sprintf(`{"external_requester":"customer-123","inventory_id":%d,"host_ids":[%d]}`,
		inventoryID, hostOutside)
	if rec := callDelegatedLaunch(t, resource.LaunchWorkflow, workflowID, "request-2", outsideBody, principal); rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-scope host: want 403, got %d (%s)", rec.Code, rec.Body)
	}
	limitBody := fmt.Sprintf(`{"external_requester":"customer-123","inventory_id":%d,"host_ids":[%d],"limit":"all"}`,
		inventoryID, hostA)
	if rec := callDelegatedLaunch(t, resource.LaunchWorkflow, workflowID, "request-3", limitBody, principal); rec.Code != http.StatusBadRequest {
		t.Fatalf("client limit: want 400, got %d (%s)", rec.Code, rec.Body)
	}
}

func callDelegatedLaunch(t *testing.T, fn handlerFn, workflowID int64, idempotencyKey, body string, uc middleware.UserContext) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/delegated/workflow-templates/%d/launch", workflowID), strings.NewReader(body))
	req.Header.Set("Idempotency-Key", idempotencyKey)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fmt.Sprint(workflowID))
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, middleware.UserContextKey, uc)
	rec := httptest.NewRecorder()
	fn(rec, req.WithContext(ctx))
	return rec
}
