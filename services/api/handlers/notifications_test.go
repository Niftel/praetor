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
