package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestDelegatedWorkflowLaunchDeniesScopeExpansion(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("delegated-deny-org-%d", uniq))
	otherOrgID := createOrg(t, db, fmt.Sprintf("delegated-deny-other-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("delegated-deny-user-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=ANY($1)`, pq.Array([]int64{orgID, otherOrgID}))
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	var principalID, credentialID, workflowID, inventoryID, otherInventoryID int64
	var allowedHostID, excessHostID, disabledHostID, otherInventoryHostID int64
	mustGet := func(dest *int64, query string, args ...interface{}) {
		t.Helper()
		if err := db.Get(dest, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGet(&principalID, `INSERT INTO service_principals
		(organization_id,name,created_by_user_id) VALUES ($1,$2,$3) RETURNING id`,
		orgID, fmt.Sprintf("delegated-deny-principal-%d", uniq), userID)
	mustGet(&credentialID, `INSERT INTO service_credentials
		(service_principal_id,name,token_hash,expires_at,created_by_user_id)
		VALUES ($1,'test',$2,now()+interval '1 hour',$3) RETURNING id`,
		principalID, fmt.Sprintf("%064d", uniq), userID)
	mustGet(&workflowID, `INSERT INTO workflow_templates
		(organization_id,name,allow_simultaneous) VALUES ($1,$2,true) RETURNING id`,
		orgID, fmt.Sprintf("delegated-deny-workflow-%d", uniq))
	mustGet(&inventoryID, `INSERT INTO inventories
		(organization_id,name,kind) VALUES ($1,$2,'static') RETURNING id`,
		orgID, fmt.Sprintf("delegated-deny-inventory-%d", uniq))
	mustGet(&otherInventoryID, `INSERT INTO inventories
		(organization_id,name,kind) VALUES ($1,$2,'static') RETURNING id`,
		otherOrgID, fmt.Sprintf("delegated-deny-other-inventory-%d", uniq))
	mustGet(&allowedHostID, `INSERT INTO hosts (inventory_id,name) VALUES ($1,'allowed-01') RETURNING id`, inventoryID)
	mustGet(&excessHostID, `INSERT INTO hosts (inventory_id,name) VALUES ($1,'excess-01') RETURNING id`, inventoryID)
	mustGet(&disabledHostID, `INSERT INTO hosts (inventory_id,name,enabled) VALUES ($1,'disabled-01',false) RETURNING id`, inventoryID)
	mustGet(&otherInventoryHostID, `INSERT INTO hosts (inventory_id,name) VALUES ($1,'other-01') RETURNING id`, otherInventoryID)
	if _, err := db.Exec(`INSERT INTO delegated_launch_grants
		(organization_id,service_principal_id,workflow_template_id,inventory_id,
		 allowed_host_ids,max_hosts,allowed_extra_var_keys,expires_at,created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,1,$6,now()+interval '1 hour',$7)`,
		orgID, principalID, workflowID, inventoryID, pq.Array([]int64{allowedHostID}),
		pq.Array([]string{"ticket"}), userID); err != nil {
		t.Fatal(err)
	}

	resource := handlers.NewDelegatedLaunchResource(db)
	principal := middleware.UserContext{
		Kind: middleware.ServicePrincipal, Username: "bounded-client",
		ServicePrincipalID: principalID, ServiceCredentialID: credentialID,
		OrganizationID: orgID,
	}
	body := func(inventory int64, hosts []int64, suffix string) string {
		return fmt.Sprintf(`{"external_requester":"requester-%s","inventory_id":%d,"host_ids":%s}`,
			suffix, inventory, mustJSON(t, hosts))
	}

	tests := []struct {
		name string
		body string
		want int
	}{
		{"allowed host", body(inventoryID, []int64{allowedHostID}, "allowed"), http.StatusCreated},
		{"host outside allowlist", body(inventoryID, []int64{excessHostID}, "allowlist"), http.StatusForbidden},
		{"disabled host", body(inventoryID, []int64{disabledHostID}, "disabled"), http.StatusForbidden},
		{"host from another inventory", body(inventoryID, []int64{otherInventoryHostID}, "host-inventory"), http.StatusForbidden},
		{"cross organization inventory", body(otherInventoryID, []int64{otherInventoryHostID}, "cross-org"), http.StatusForbidden},
		{"host count exceeds grant", body(inventoryID, []int64{allowedHostID, excessHostID}, "max-hosts"), http.StatusForbidden},
		{"variable outside allowlist", fmt.Sprintf(`{"external_requester":"requester-var","inventory_id":%d,"host_ids":[%d],"extra_vars":{"become_password":"nope"}}`, inventoryID, allowedHostID), http.StatusForbidden},
		{"raw limit rejected", fmt.Sprintf(`{"external_requester":"requester-limit","inventory_id":%d,"host_ids":[%d],"limit":"all"}`, inventoryID, allowedHostID), http.StatusBadRequest},
		{"organization id rejected", fmt.Sprintf(`{"external_requester":"requester-org","organization_id":%d,"inventory_id":%d,"host_ids":[%d]}`, otherOrgID, inventoryID, allowedHostID), http.StatusBadRequest},
		{"approval team rejected", fmt.Sprintf(`{"external_requester":"requester-team","approval_team_id":1,"inventory_id":%d,"host_ids":[%d]}`, inventoryID, allowedHostID), http.StatusBadRequest},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := callDelegatedLaunch(t, resource.LaunchWorkflow, workflowID,
				fmt.Sprintf("deny-%d-%d", uniq, i), tt.body, principal)
			if rec.Code != tt.want {
				t.Fatalf("want %d, got %d (%s)", tt.want, rec.Code, rec.Body)
			}
		})
	}
}

func TestDelegatedWorkflowLaunchConcurrentIdempotency(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("delegated-concurrent-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("delegated-concurrent-user-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	var principalID, credentialID, workflowID, inventoryID, hostID int64
	mustGet := func(dest *int64, query string, args ...interface{}) {
		t.Helper()
		if err := db.Get(dest, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGet(&principalID, `INSERT INTO service_principals
		(organization_id,name,created_by_user_id) VALUES ($1,$2,$3) RETURNING id`,
		orgID, fmt.Sprintf("delegated-concurrent-principal-%d", uniq), userID)
	mustGet(&credentialID, `INSERT INTO service_credentials
		(service_principal_id,name,token_hash,expires_at,created_by_user_id)
		VALUES ($1,'test',$2,now()+interval '1 hour',$3) RETURNING id`,
		principalID, fmt.Sprintf("%064d", uniq), userID)
	mustGet(&workflowID, `INSERT INTO workflow_templates
		(organization_id,name,allow_simultaneous) VALUES ($1,$2,false) RETURNING id`,
		orgID, fmt.Sprintf("delegated-concurrent-workflow-%d", uniq))
	mustGet(&inventoryID, `INSERT INTO inventories
		(organization_id,name,kind) VALUES ($1,$2,'static') RETURNING id`,
		orgID, fmt.Sprintf("delegated-concurrent-inventory-%d", uniq))
	mustGet(&hostID, `INSERT INTO hosts (inventory_id,name) VALUES ($1,'concurrent-01') RETURNING id`, inventoryID)
	if _, err := db.Exec(`INSERT INTO delegated_launch_grants
		(organization_id,service_principal_id,workflow_template_id,inventory_id,
		 allowed_host_ids,expires_at,created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,now()+interval '1 hour',$6)`,
		orgID, principalID, workflowID, inventoryID, pq.Array([]int64{hostID}), userID); err != nil {
		t.Fatal(err)
	}

	resource := handlers.NewDelegatedLaunchResource(db)
	principal := middleware.UserContext{
		Kind: middleware.ServicePrincipal, Username: "concurrent-client",
		ServicePrincipalID: principalID, ServiceCredentialID: credentialID,
		OrganizationID: orgID,
	}
	body := fmt.Sprintf(`{"external_requester":"concurrent-user","inventory_id":%d,"host_ids":[%d]}`, inventoryID, hostID)

	t.Run("same key creates exactly one run", func(t *testing.T) {
		responses := concurrentDelegatedLaunches(t, 2, func(i int) *httptest.ResponseRecorder {
			return callDelegatedLaunch(t, resource.LaunchWorkflow, workflowID, "same-key", body, principal)
		})
		assertStatusCounts(t, responses, map[int]int{http.StatusCreated: 1, http.StatusOK: 1})
		var runIDs = map[int64]struct{}{}
		for _, rec := range responses {
			var response struct {
				WorkflowJobID int64 `json:"workflow_job_id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			runIDs[response.WorkflowJobID] = struct{}{}
		}
		if len(runIDs) != 1 {
			t.Fatalf("same idempotency key created multiple runs: %v", runIDs)
		}
	})

	if _, err := db.Exec(`UPDATE workflow_jobs SET status='successful' WHERE workflow_template_id=$1`, workflowID); err != nil {
		t.Fatal(err)
	}
	t.Run("different keys cannot bypass concurrency guard", func(t *testing.T) {
		responses := concurrentDelegatedLaunches(t, 2, func(i int) *httptest.ResponseRecorder {
			return callDelegatedLaunch(t, resource.LaunchWorkflow, workflowID,
				fmt.Sprintf("different-key-%d", i), body, principal)
		})
		assertStatusCounts(t, responses, map[int]int{http.StatusCreated: 1, http.StatusConflict: 1})
	})
}

func TestDelegatedWorkflowLaunchHTTPPrincipalBoundary(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("delegated-http-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("delegated-http-user-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	serviceToken := middleware.ServiceTokenPrefix + fmt.Sprintf("%043d", uniq)
	patToken := middleware.PATPrefix + fmt.Sprintf("%043d", uniq)
	var principalID, workflowID, inventoryID, hostID int64
	mustGet := func(dest *int64, query string, args ...interface{}) {
		t.Helper()
		if err := db.Get(dest, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGet(&principalID, `INSERT INTO service_principals
		(organization_id,name,created_by_user_id) VALUES ($1,$2,$3) RETURNING id`,
		orgID, fmt.Sprintf("delegated-http-principal-%d", uniq), userID)
	if _, err := db.Exec(`INSERT INTO service_credentials
		(service_principal_id,name,token_hash,expires_at,created_by_user_id)
		VALUES ($1,'test',$2,now()+interval '1 hour',$3)`,
		principalID, middleware.HashToken(serviceToken), userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO api_tokens
		(user_id,name,token_hash,expires_at) VALUES ($1,'test',$2,now()+interval '1 hour')`,
		userID, middleware.HashToken(patToken)); err != nil {
		t.Fatal(err)
	}
	mustGet(&workflowID, `INSERT INTO workflow_templates
		(organization_id,name,allow_simultaneous) VALUES ($1,$2,true) RETURNING id`,
		orgID, fmt.Sprintf("delegated-http-workflow-%d", uniq))
	mustGet(&inventoryID, `INSERT INTO inventories
		(organization_id,name,kind) VALUES ($1,$2,'static') RETURNING id`,
		orgID, fmt.Sprintf("delegated-http-inventory-%d", uniq))
	mustGet(&hostID, `INSERT INTO hosts (inventory_id,name) VALUES ($1,'http-01') RETURNING id`, inventoryID)
	if _, err := db.Exec(`INSERT INTO delegated_launch_grants
		(organization_id,service_principal_id,workflow_template_id,inventory_id,
		 allowed_host_ids,expires_at,created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,now()+interval '1 hour',$6)`,
		orgID, principalID, workflowID, inventoryID, pq.Array([]int64{hostID}), userID); err != nil {
		t.Fatal(err)
	}

	resource := handlers.NewDelegatedLaunchResource(db)
	router := chi.NewRouter()
	router.With(middleware.AuthMiddleware(db), middleware.RequireService).
		Post("/api/v1/delegated/workflow-templates/{id}/launch", resource.LaunchWorkflow)
	router.With(middleware.AuthMiddleware(db), middleware.RequireHuman).
		Get("/api/v1/human-only", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	body := fmt.Sprintf(`{"external_requester":"http-user","inventory_id":%d,"host_ids":[%d]}`, inventoryID, hostID)
	request := func(method, path, token, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	launchPath := fmt.Sprintf("/api/v1/delegated/workflow-templates/%d/launch", workflowID)
	if rec := request(http.MethodPost, launchPath, serviceToken, "http-service", body); rec.Code != http.StatusCreated {
		t.Fatalf("service credential delegated launch: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	if rec := request(http.MethodGet, "/api/v1/human-only", serviceToken, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("service credential human API: want 403, got %d (%s)", rec.Code, rec.Body)
	}
	if rec := request(http.MethodPost, launchPath, patToken, "http-human", body); rec.Code != http.StatusForbidden {
		t.Fatalf("human PAT delegated launch: want 403, got %d (%s)", rec.Code, rec.Body)
	}
}

func mustJSON(t *testing.T, value interface{}) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func concurrentDelegatedLaunches(t *testing.T, count int, launch func(int) *httptest.ResponseRecorder) []*httptest.ResponseRecorder {
	t.Helper()
	responses := make([]*httptest.ResponseRecorder, count)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			responses[i] = launch(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return responses
}

func assertStatusCounts(t *testing.T, responses []*httptest.ResponseRecorder, want map[int]int) {
	t.Helper()
	got := make(map[int]int)
	for _, rec := range responses {
		got[rec.Code]++
	}
	matches := len(got) == len(want)
	for status, count := range want {
		matches = matches && got[status] == count
	}
	if !matches {
		bodies := make([]string, 0, len(responses))
		for _, rec := range responses {
			bodies = append(bodies, rec.Body.String())
		}
		t.Fatalf("status counts: want %v, got %v (%v)", want, got, bodies)
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
