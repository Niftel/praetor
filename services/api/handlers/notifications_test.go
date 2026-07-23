package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

func TestNotificationTargetLifecycleRedactsAndTestsDelivery(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	t.Setenv("PRAETOR_ALLOW_INSECURE_DEFAULTS", "true")

	var deliveries atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveries.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read notification body: %v", err)
		}
		if !strings.Contains(string(body), `"event":"test"`) {
			t.Errorf("test delivery body = %s", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	parsed, err := url.Parse(receiver.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PRAETOR_NOTIFICATION_ALLOWED_HOSTS", parsed.Hostname())

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("notification-org-%d", uniq))
	adminID := createUser(t, db, fmt.Sprintf("notification-admin-%d", uniq))
	readerID := createUser(t, db, fmt.Sprintf("notification-reader-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, adminID, readerID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
	})

	resource := handlers.NewNotificationsResource(db, handlers.NewAuthorizer(db))
	admin := middleware.UserContext{UserID: adminID, IsSuperuser: true}
	reader := middleware.UserContext{UserID: readerID}
	createBody := fmt.Sprintf(`{"organization_id":%d,"name":"Platform alerts","notification_type":"webhook","config":{"url":%q}}`, orgID, receiver.URL)
	rec := callJSON(t, resource.CreateNotificationTemplate, http.MethodPost, createBody, admin, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create target: status %d (%s)", rec.Code, rec.Body)
	}
	id := extractID(t, rec.Body.String())

	var stored string
	if err := db.Get(&stored, `SELECT config::text FROM notification_templates WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, receiver.URL) {
		t.Fatalf("stored notification config contains plaintext destination: %s", stored)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?organization_id=%d", orgID), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, admin))
	rec = httptest.NewRecorder()
	resource.ListNotificationTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list targets: status %d (%s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), receiver.URL) || strings.Contains(rec.Body.String(), "config") {
		t.Fatalf("list response exposed target config: %s", rec.Body)
	}

	rec = callJSON(t, resource.TestNotificationTemplate, http.MethodPost, "", reader, map[string]string{"id": fmt.Sprint(id)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized test: status %d, want 403", rec.Code)
	}
	if got := deliveries.Load(); got != 0 {
		t.Fatalf("unauthorized test delivered %d notifications", got)
	}

	rec = callJSON(t, resource.TestNotificationTemplate, http.MethodPost, "", admin, map[string]string{"id": fmt.Sprint(id)})
	if rec.Code != http.StatusOK {
		t.Fatalf("test target: status %d (%s)", rec.Code, rec.Body)
	}
	var result struct {
		Status                 string `json:"status"`
		NotificationTemplateID int64  `json:"notification_template_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "delivered" || result.NotificationTemplateID != id {
		t.Fatalf("test result = %#v", result)
	}
	if got := deliveries.Load(); got != 1 {
		t.Fatalf("test delivered %d notifications, want 1", got)
	}
}

func TestCreateNotificationTargetRejectsUnsafeDestination(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	t.Setenv("PRAETOR_ALLOW_INSECURE_DEFAULTS", "true")
	t.Setenv("PRAETOR_NOTIFICATION_ALLOWED_HOSTS", "")

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("notification-unsafe-org-%d", uniq))
	adminID := createUser(t, db, fmt.Sprintf("notification-unsafe-admin-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, adminID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
	})

	resource := handlers.NewNotificationsResource(db, handlers.NewAuthorizer(db))
	body := fmt.Sprintf(`{"organization_id":%d,"name":"Unsafe","notification_type":"webhook","config":{"url":"http://169.254.169.254/latest/meta-data"}}`, orgID)
	rec := callJSON(t, resource.CreateNotificationTemplate, http.MethodPost, body, middleware.UserContext{UserID: adminID, IsSuperuser: true}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsafe target: status %d (%s), want 400", rec.Code, rec.Body)
	}
}

func TestNotificationRBACRedactionAndActivityBoundaries(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	t.Setenv("PRAETOR_ALLOW_INSECURE_DEFAULTS", "true")

	var deliveries atomic.Int32
	const secretCanary = "notification-secret-canary"
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deliveries.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	parsed, err := url.Parse(receiver.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PRAETOR_NOTIFICATION_ALLOWED_HOSTS", parsed.Hostname())

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("notification-boundary-org-%d", uniq))
	adminID := createUser(t, db, fmt.Sprintf("notification-boundary-admin-%d", uniq))
	auditorID := createUser(t, db, fmt.Sprintf("notification-boundary-auditor-%d", uniq))
	memberID := createUser(t, db, fmt.Sprintf("notification-boundary-member-%d", uniq))
	access := rbac.NewStore(db, testResourceTables)
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.AdminRole, adminID)
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.AuditorRole, auditorID)
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.MemberRole, memberID)
	systemAuditor, err := access.RoleByName(context.Background(), rbac.SystemAuditor)
	if err != nil {
		t.Fatal(err)
	}
	if err := access.Assign(context.Background(), rbac.Assignment{
		RoleDefinitionID: systemAuditor.ID,
		PrincipalKind:    rbac.UserPrincipal,
		PrincipalID:      auditorID,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, adminID, auditorID, memberID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
	})

	resource := handlers.NewNotificationsResource(db, handlers.NewAuthorizer(db))
	admin := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: adminID, Username: "notification-admin"}
	auditor := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: auditorID, Username: "notification-auditor"}
	member := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: memberID, Username: "notification-member"}
	createBody := fmt.Sprintf(
		`{"organization_id":%d,"name":"Boundary target","notification_type":"webhook","config":{"url":%q}}`,
		orgID, receiver.URL+"/"+secretCanary,
	)
	rec := callAuditedJSON(t, db, resource.CreateNotificationTemplate, http.MethodPost,
		"/api/v1/notification-templates", createBody, admin, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create target: status %d (%s)", rec.Code, rec.Body)
	}
	targetID := extractID(t, rec.Body.String())

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?organization_id=%d", orgID), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, auditor))
	rec = httptest.NewRecorder()
	resource.ListNotificationTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auditor list targets: status %d (%s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), secretCanary) || strings.Contains(rec.Body.String(), receiver.URL) ||
		strings.Contains(rec.Body.String(), `"config"`) {
		t.Fatalf("auditor response exposed target config: %s", rec.Body)
	}

	for name, principal := range map[string]middleware.UserContext{
		"auditor": auditor,
		"member":  member,
		"service-principal": {
			Kind: middleware.ServicePrincipal, OrganizationID: orgID, Username: "notification-service",
		},
	} {
		t.Run(name+" cannot test target", func(t *testing.T) {
			rec := callAuditedJSON(t, db, resource.TestNotificationTemplate, http.MethodPost,
				fmt.Sprintf("/api/v1/notification-templates/%d/test", targetID), "", principal,
				map[string]string{"id": fmt.Sprint(targetID)})
			if rec.Code != http.StatusForbidden ||
				!strings.Contains(rec.Body.String(), "notification_admin_required") {
				t.Fatalf("test denial: status %d (%s)", rec.Code, rec.Body)
			}
		})
	}
	if got := deliveries.Load(); got != 0 {
		t.Fatalf("unauthorized principals delivered %d test notifications", got)
	}

	var workflowID, teamID int64
	if err := db.Get(&workflowID, `INSERT INTO workflow_templates (organization_id,name) VALUES ($1,$2) RETURNING id`,
		orgID, fmt.Sprintf("notification-boundary-workflow-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&teamID, `INSERT INTO teams (organization_id,name) VALUES ($1,$2) RETURNING id`,
		orgID, fmt.Sprintf("notification-boundary-team-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	policyBody := fmt.Sprintf(
		`{"notification_template_id":%d,"resource_type":"workflow_template","resource_id":%d,"team_id":%d,"event":"approval"}`,
		targetID, workflowID, teamID,
	)
	rec = callAuditedJSON(t, db, resource.CreateNotificationPolicy, http.MethodPost,
		"/api/v1/notification-policies", policyBody, admin, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create notification policy: status %d (%s)", rec.Code, rec.Body)
	}
	policyID := extractID(t, rec.Body.String())
	rec = callAuditedJSON(t, db, resource.DeleteNotificationPolicy, http.MethodDelete,
		fmt.Sprintf("/api/v1/notification-policies/%d", policyID), "", auditor,
		map[string]string{"id": fmt.Sprint(policyID)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("auditor delete notification policy: status %d (%s)", rec.Code, rec.Body)
	}

	rec = callAuditedJSON(t, db, resource.TestNotificationTemplate, http.MethodPost,
		fmt.Sprintf("/api/v1/notification-templates/%d/test", targetID), "", admin,
		map[string]string{"id": fmt.Sprint(targetID)})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin test target: status %d (%s)", rec.Code, rec.Body)
	}

	activity := handlers.NewAccessResource(db, handlers.NewAuthorizer(db))
	req = httptest.NewRequest(http.MethodGet, "/activity-stream?limit=30", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, auditor))
	rec = httptest.NewRecorder()
	activity.ListActivityStream(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("system auditor activity stream: status %d (%s)", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, required := range []string{
		`"organization_id":`, `"outcome":"success"`, `"outcome":"denied"`,
		`"failure_code":"notification_admin_required"`, `"failure_code":"permission_denied"`,
		`"principal_kind":"service"`, `"resource_type":"notification_policy"`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("activity stream missing %s: %s", required, body)
		}
	}
	for _, forbidden := range []string{secretCanary, receiver.URL, `"config"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("activity stream exposed %q: %s", forbidden, body)
		}
	}
}

func callAuditedJSON(t *testing.T, db *sqlx.DB, fn handlerFn, method, path, body string, uc middleware.UserContext, urlParams map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var activityBefore int
	if err := db.Get(&activityBefore, `SELECT count(*) FROM activity_stream`); err != nil {
		t.Fatalf("count activity before request: %v", err)
	}
	recorder := middleware.NewActivityRecorder(context.Background(), db, time.Second)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	routeContext := chi.NewRouteContext()
	for key, value := range urlParams {
		routeContext.URLParams.Add(key, value)
	}
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	ctx = context.WithValue(ctx, middleware.UserContextKey, uc)
	rec := httptest.NewRecorder()
	recorder.Middleware(http.HandlerFunc(fn)).ServeHTTP(rec, request.WithContext(ctx))
	deadline := time.Now().Add(time.Second)
	for {
		var activityAfter int
		if err := db.Get(&activityAfter, `SELECT count(*) FROM activity_stream`); err != nil {
			t.Fatalf("count activity after request: %v", err)
		}
		if activityAfter > activityBefore {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("activity record was not persisted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Close(closeContext); err != nil {
		t.Fatalf("close activity recorder: %v", err)
	}
	return rec
}

func TestNotificationDeliveryHistoryPaginationRBACAndRetention(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("delivery-history-org-%d", uniq))
	otherOrgID := createOrg(t, db, fmt.Sprintf("delivery-history-other-org-%d", uniq))
	adminID := createUser(t, db, fmt.Sprintf("delivery-history-admin-%d", uniq))
	teamReaderID := createUser(t, db, fmt.Sprintf("delivery-history-team-reader-%d", uniq))
	orgReaderID := createUser(t, db, fmt.Sprintf("delivery-history-org-reader-%d", uniq))
	otherReaderID := createUser(t, db, fmt.Sprintf("delivery-history-other-reader-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4)`, adminID, teamReaderID, orgReaderID, otherReaderID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id IN ($1,$2)`, orgID, otherOrgID)
	})

	var teamID, workflowID, targetID, policyID int64
	if err := db.Get(&teamID, `INSERT INTO teams (organization_id,name) VALUES ($1,$2) RETURNING id`, orgID, fmt.Sprintf("delivery-team-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&workflowID, `INSERT INTO workflow_templates (organization_id,name) VALUES ($1,$2) RETURNING id`, orgID, fmt.Sprintf("delivery-workflow-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&targetID, `
		INSERT INTO notification_templates (organization_id,name,notification_type,config)
		VALUES ($1,$2,'webhook','{"url":"encrypted-secret-path"}') RETURNING id`,
		orgID, fmt.Sprintf("delivery-target-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&policyID, `
		INSERT INTO notification_policies (
			organization_id,team_id,notification_template_id,resource_type,resource_id,event
		) VALUES ($1,$2,$3,'workflow_template',$4,'approval') RETURNING id`,
		orgID, teamID, targetID, workflowID); err != nil {
		t.Fatal(err)
	}

	insertDelivery := func(key, occurrence, status string) int64 {
		t.Helper()
		var id int64
		deliveredAt := "NULL"
		if status == "delivered" {
			deliveredAt = "now()"
		}
		query := fmt.Sprintf(`
			INSERT INTO notification_deliveries (
				idempotency_key,organization_id,team_id,notification_policy_id,notification_template_id,
				target_name,target_type,resource_type,resource_id,event,
				occurrence_type,occurrence_id,subject_id,subject_name,subject_kind,
				status,attempt_count,first_attempt_at,last_attempt_at,delivered_at
			) VALUES ($1,$2,$3,$4,$5,'ignored','ignored','workflow_template',$6,'approval',
			          'workflow_node',$7,$8,$9,'workflow approval',$10,1,now(),now(),%s)
			RETURNING id`, deliveredAt)
		if err := db.Get(&id, query, key, orgID, teamID, policyID, targetID, workflowID,
			occurrence, workflowID, fmt.Sprintf("Release workflow %s", occurrence), status); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			INSERT INTO notification_delivery_attempts (
				delivery_id,attempt_number,outcome,failure_code,failure_reason,started_at,finished_at
			) VALUES ($1,1,$2,$3,$4,now()-interval '1 second',now())`,
			id,
			map[bool]string{true: "delivered", false: "transient_failure"}[status == "delivered"],
			map[bool]any{true: nil, false: "endpoint_unavailable"}[status == "delivered"],
			map[bool]any{true: nil, false: "Destination temporarily unavailable"}[status == "delivered"]); err != nil {
			t.Fatal(err)
		}
		return id
	}
	firstID := insertDelivery(fmt.Sprintf("workflow:%d:node:1:approval:policy:%d", workflowID, policyID), "node-1", "retrying")
	secondID := insertDelivery(fmt.Sprintf("workflow:%d:node:2:approval:policy:%d", workflowID, policyID), "node-2", "delivered")

	cloneDelivery := `
		INSERT INTO notification_deliveries (
			idempotency_key,organization_id,team_id,notification_policy_id,notification_template_id,
			target_name,target_type,resource_type,resource_id,event,
			occurrence_type,occurrence_id,subject_id,subject_name,subject_kind,
			status,attempt_count,max_attempts,next_attempt_at,
			first_attempt_at,last_attempt_at,delivered_at,failed_at,failure_code,failure_reason
		)
		SELECT idempotency_key,$2,team_id,notification_policy_id,notification_template_id,
		       target_name,target_type,resource_type,resource_id,event,
		       occurrence_type,occurrence_id,subject_id,subject_name,subject_kind,
		       status,attempt_count,max_attempts,next_attempt_at,
		       first_attempt_at,last_attempt_at,delivered_at,failed_at,failure_code,failure_reason
		  FROM notification_deliveries WHERE id=$1
		ON CONFLICT (idempotency_key) DO NOTHING`
	result, err := db.Exec(cloneDelivery, firstID, orgID)
	if err != nil {
		t.Fatalf("idempotent delivery replay: %v", err)
	}
	if changed, _ := result.RowsAffected(); changed != 0 {
		t.Fatalf("idempotent delivery replay inserted %d rows, want 0", changed)
	}
	if _, err := db.Exec(cloneDelivery, firstID, otherOrgID); err == nil {
		t.Fatal("cross-organization delivery scope unexpectedly passed")
	}
	if _, err := db.Exec(`UPDATE notification_deliveries SET status='sending' WHERE id=$1`, firstID); err == nil {
		t.Fatal("sending delivery without a lease unexpectedly passed")
	}
	if _, err := db.Exec(`
		UPDATE notification_deliveries
		   SET status='sending', lease_owner='consumer-a', lease_expires_at=now()+interval '30 seconds'
		 WHERE id=$1`, firstID); err != nil {
		t.Fatalf("valid delivery lease: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE notification_deliveries
		   SET status='retrying', lease_owner=NULL, lease_expires_at=NULL
		 WHERE id=$1`, firstID); err != nil {
		t.Fatalf("release delivery lease: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO notification_delivery_attempts (
			delivery_id,attempt_number,outcome,failure_code,failure_reason,started_at,finished_at
		) VALUES ($1,1,'transient_failure','duplicate','duplicate',now(),now())`, firstID); err == nil {
		t.Fatal("duplicate attempt number unexpectedly passed")
	}

	access := rbac.NewStore(db, testResourceTables)
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.ReadRole, teamReaderID)
	grantObjectRole(t, access, rbac.Team, teamID, rbac.ReadRole, teamReaderID)
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.ReadRole, orgReaderID)
	grantObjectRole(t, access, rbac.Organization, otherOrgID, rbac.ReadRole, otherReaderID)
	if _, err := db.Exec(`INSERT INTO team_members (team_id,user_id) VALUES ($1,$2)`, teamID, teamReaderID); err != nil {
		t.Fatal(err)
	}

	resource := handlers.NewNotificationsResource(db, handlers.NewAuthorizer(db))
	callHistory := func(user middleware.UserContext, query string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/notification-deliveries?"+query, nil)
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, user))
		rec := httptest.NewRecorder()
		resource.ListNotificationDeliveryHistory(rec, req)
		return rec
	}

	admin := middleware.UserContext{UserID: adminID, IsSuperuser: true}
	rec := callHistory(admin, fmt.Sprintf("organization_id=%d&limit=1", orgID))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin history: status %d (%s)", rec.Code, rec.Body)
	}
	var page struct {
		Results []struct {
			ID       int64  `json:"id"`
			Status   string `json:"status"`
			Attempts []struct {
				Outcome string `json:"outcome"`
			} `json:"attempts"`
		} `json:"results"`
		NextCursor *int64 `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 1 || page.Results[0].ID != secondID || page.NextCursor == nil || *page.NextCursor != secondID {
		t.Fatalf("first page = %#v body=%s", page, rec.Body)
	}
	if len(page.Results[0].Attempts) != 1 || page.Results[0].Attempts[0].Outcome != "delivered" {
		t.Fatalf("attempt history missing: %s", rec.Body)
	}
	for _, forbidden := range []string{"encrypted-secret-path", "idempotency_key", `"config"`} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("history exposed %q: %s", forbidden, rec.Body)
		}
	}

	rec = callHistory(admin, fmt.Sprintf("organization_id=%d&limit=1&cursor=%d", orgID, *page.NextCursor))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), fmt.Sprintf(`"id":%d`, firstID)) {
		t.Fatalf("second page: status=%d body=%s", rec.Code, rec.Body)
	}

	rec = callHistory(middleware.UserContext{UserID: teamReaderID}, fmt.Sprintf("organization_id=%d", orgID))
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"target_name"`) != 2 {
		t.Fatalf("team reader history: status=%d body=%s", rec.Code, rec.Body)
	}
	rec = callHistory(middleware.UserContext{UserID: orgReaderID}, fmt.Sprintf("organization_id=%d", orgID))
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"target_name"`) != 0 {
		t.Fatalf("unscoped org reader must not see team history: status=%d body=%s", rec.Code, rec.Body)
	}
	rec = callHistory(middleware.UserContext{UserID: otherReaderID}, fmt.Sprintf("organization_id=%d", orgID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-org reader: status=%d body=%s", rec.Code, rec.Body)
	}

	if _, err := db.Exec(`DELETE FROM notification_templates WHERE id=$1`, targetID); err != nil {
		t.Fatal(err)
	}
	rec = callHistory(admin, fmt.Sprintf("organization_id=%d", orgID))
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"target_name"`) != 2 ||
		!strings.Contains(rec.Body.String(), fmt.Sprintf("delivery-target-%d", uniq)) {
		t.Fatalf("history was not retained after target deletion: status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestNotificationPoliciesEnforceApprovalTeamAndOrganizationScope(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("policy-org-%d", uniq))
	otherOrgID := createOrg(t, db, fmt.Sprintf("policy-other-org-%d", uniq))
	adminID := createUser(t, db, fmt.Sprintf("policy-admin-%d", uniq))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, adminID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id IN ($1,$2)`, orgID, otherOrgID)
	})

	var workflowID, teamID, targetID, otherTargetID int64
	if err := db.Get(&workflowID, `INSERT INTO workflow_templates (organization_id,name) VALUES ($1,$2) RETURNING id`, orgID, fmt.Sprintf("policy-workflow-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&teamID, `INSERT INTO teams (organization_id,name) VALUES ($1,$2) RETURNING id`, orgID, fmt.Sprintf("policy-team-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&targetID, `INSERT INTO notification_templates (organization_id,name,notification_type) VALUES ($1,$2,'webhook') RETURNING id`, orgID, fmt.Sprintf("policy-target-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&otherTargetID, `INSERT INTO notification_templates (organization_id,name,notification_type) VALUES ($1,$2,'webhook') RETURNING id`, otherOrgID, fmt.Sprintf("policy-other-target-%d", uniq)); err != nil {
		t.Fatal(err)
	}

	resource := handlers.NewNotificationsResource(db, handlers.NewAuthorizer(db))
	admin := middleware.UserContext{UserID: adminID, IsSuperuser: true}
	unscoped := fmt.Sprintf(`{"notification_template_id":%d,"resource_type":"workflow_template","resource_id":%d,"event":"approval"}`, targetID, workflowID)
	rec := callJSON(t, resource.CreateNotificationPolicy, http.MethodPost, unscoped, admin, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unscoped approval policy: status %d (%s), want 400", rec.Code, rec.Body)
	}

	crossOrg := fmt.Sprintf(`{"notification_template_id":%d,"resource_type":"workflow_template","resource_id":%d,"team_id":%d,"event":"approval"}`, otherTargetID, workflowID, teamID)
	rec = callJSON(t, resource.CreateNotificationPolicy, http.MethodPost, crossOrg, admin, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-org approval policy: status %d (%s), want 400", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "notification_scope_invalid") {
		t.Fatalf("cross-org approval policy returned unstable reason: %s", rec.Body)
	}

	valid := fmt.Sprintf(`{"notification_template_id":%d,"resource_type":"workflow_template","resource_id":%d,"team_id":%d,"event":"approval"}`, targetID, workflowID, teamID)
	rec = callJSON(t, resource.CreateNotificationPolicy, http.MethodPost, valid, admin, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create approval policy: status %d (%s)", rec.Code, rec.Body)
	}
	policyID := extractID(t, rec.Body.String())

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?resource_type=workflow_template&resource_id=%d", workflowID), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, admin))
	rec = httptest.NewRecorder()
	resource.ListNotificationPolicies(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list approval policies: status %d (%s)", rec.Code, rec.Body)
	}
	var policies []struct {
		ID                     int64  `json:"id"`
		TeamID                 int64  `json:"team_id"`
		NotificationTemplateID int64  `json:"notification_template_id"`
		Event                  string `json:"event"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &policies); err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0].ID != policyID || policies[0].TeamID != teamID || policies[0].NotificationTemplateID != targetID || policies[0].Event != "approval" {
		t.Fatalf("approval policies = %#v", policies)
	}

	rec = callJSON(t, resource.DeleteNotificationPolicy, http.MethodDelete, "", admin, map[string]string{"id": fmt.Sprint(policyID)})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete approval policy: status %d (%s)", rec.Code, rec.Body)
	}
	var remaining int
	if err := db.Get(&remaining, `SELECT count(*) FROM notification_policies WHERE id=$1`, policyID); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("deleted policy still exists")
	}
}

func TestLegacyNotificationAttachmentsSyncToCommonPolicies(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("legacy-policy-org-%d", uniq))
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID) })

	var workflowID, targetID int64
	if err := db.Get(&workflowID, `INSERT INTO workflow_templates (organization_id,name) VALUES ($1,$2) RETURNING id`, orgID, fmt.Sprintf("legacy-policy-workflow-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&targetID, `INSERT INTO notification_templates (organization_id,name,notification_type) VALUES ($1,$2,'webhook') RETURNING id`, orgID, fmt.Sprintf("legacy-policy-target-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO teams (organization_id,name) VALUES ($1,$2),($1,$3)`, orgID, fmt.Sprintf("legacy-policy-team-a-%d", uniq), fmt.Sprintf("legacy-policy-team-b-%d", uniq)); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`INSERT INTO workflow_template_notifications (workflow_template_id,notification_template_id,event) VALUES ($1,$2,'approval')`, workflowID, targetID); err != nil {
		t.Fatal(err)
	}
	var scopedCount int
	if err := db.Get(&scopedCount, `SELECT count(*) FROM notification_policies WHERE resource_type='workflow_template' AND resource_id=$1 AND notification_template_id=$2 AND event='approval' AND team_id IS NOT NULL`, workflowID, targetID); err != nil {
		t.Fatal(err)
	}
	if scopedCount != 2 {
		t.Fatalf("legacy approval attachment produced %d team policies, want 2", scopedCount)
	}

	if _, err := db.Exec(`DELETE FROM workflow_template_notifications WHERE workflow_template_id=$1 AND notification_template_id=$2 AND event='approval'`, workflowID, targetID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&scopedCount, `SELECT count(*) FROM notification_policies WHERE resource_type='workflow_template' AND resource_id=$1 AND notification_template_id=$2 AND event='approval'`, workflowID, targetID); err != nil {
		t.Fatal(err)
	}
	if scopedCount != 0 {
		t.Fatalf("legacy detach left %d common policies", scopedCount)
	}

	var policyID int64
	if err := db.Get(&policyID, `INSERT INTO notification_policies
		(organization_id,notification_template_id,resource_type,resource_id,event)
		VALUES ($1,$2,'workflow_template',$3,'success') RETURNING id`, orgID, targetID, workflowID); err != nil {
		t.Fatal(err)
	}
	var legacyCount int
	if err := db.Get(&legacyCount, `SELECT count(*) FROM workflow_template_notifications WHERE workflow_template_id=$1 AND notification_template_id=$2 AND event='success'`, workflowID, targetID); err != nil {
		t.Fatal(err)
	}
	if legacyCount != 1 {
		t.Fatalf("common lifecycle policy produced %d legacy attachments, want 1", legacyCount)
	}
	if _, err := db.Exec(`DELETE FROM notification_policies WHERE id=$1`, policyID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&legacyCount, `SELECT count(*) FROM workflow_template_notifications WHERE workflow_template_id=$1 AND notification_template_id=$2 AND event='success'`, workflowID, targetID); err != nil {
		t.Fatal(err)
	}
	if legacyCount != 0 {
		t.Fatalf("common lifecycle policy delete left %d legacy attachments", legacyCount)
	}
}

func TestNotificationPoliciesAreDeletedWithTheirResources(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("policy-cleanup-org-%d", uniq))
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID) })

	var targetID, inventoryID, sourceID, unifiedTemplateID, jobTemplateID, workflowID int64
	if err := db.Get(&targetID, `INSERT INTO notification_templates (organization_id,name,notification_type) VALUES ($1,$2,'webhook') RETURNING id`, orgID, fmt.Sprintf("policy-cleanup-target-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&inventoryID, `INSERT INTO inventories (organization_id,name) VALUES ($1,$2) RETURNING id`, orgID, fmt.Sprintf("policy-cleanup-inventory-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&sourceID, `INSERT INTO inventory_sources (inventory_id,name) VALUES ($1,$2) RETURNING id`, inventoryID, fmt.Sprintf("policy-cleanup-source-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&unifiedTemplateID, `INSERT INTO unified_job_templates (name) VALUES ($1) RETURNING id`, fmt.Sprintf("policy-cleanup-unified-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&jobTemplateID, `INSERT INTO job_templates (organization_id,name,playbook,unified_job_template_id) VALUES ($1,$2,'site.yml',$3) RETURNING id`, orgID, fmt.Sprintf("policy-cleanup-job-%d", uniq), unifiedTemplateID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&workflowID, `INSERT INTO workflow_templates (organization_id,name) VALUES ($1,$2) RETURNING id`, orgID, fmt.Sprintf("policy-cleanup-workflow-%d", uniq)); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`INSERT INTO notification_policies (organization_id,notification_template_id,resource_type,resource_id,event) VALUES
		($1,$2,'job_template',$3,'success'),
		($1,$2,'workflow_template',$4,'success'),
		($1,$2,'inventory_source',$5,'success')`, orgID, targetID, jobTemplateID, workflowID, sourceID); err != nil {
		t.Fatal(err)
	}
	for _, deletion := range []struct {
		query string
		id    int64
	}{
		{`DELETE FROM job_templates WHERE id=$1`, jobTemplateID},
		{`DELETE FROM workflow_templates WHERE id=$1`, workflowID},
		{`DELETE FROM inventory_sources WHERE id=$1`, sourceID},
	} {
		if _, err := db.Exec(deletion.query, deletion.id); err != nil {
			t.Fatal(err)
		}
	}
	var remaining int
	if err := db.Get(&remaining, `SELECT count(*) FROM notification_policies WHERE organization_id=$1`, orgID); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("resource deletion left %d orphan notification policies", remaining)
	}
}
