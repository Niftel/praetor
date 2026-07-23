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
