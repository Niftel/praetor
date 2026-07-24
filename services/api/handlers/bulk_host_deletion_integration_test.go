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

	"github.com/lib/pq"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

type bulkHostDeletePreviewView struct {
	ConfirmationToken string `json:"confirmation_token"`
	Results           []struct {
		Index       int    `json:"index"`
		Identifier  string `json:"identifier"`
		Status      string `json:"status"`
		HTTPStatus  int    `json:"http_status"`
		HostID      int64  `json:"host_id"`
		Name        string `json:"name"`
		InventoryID int64  `json:"inventory_id"`
		Code        string `json:"code"`
		Blockers    []struct {
			Code  string `json:"code"`
			Count int    `json:"count"`
		} `json:"blocking_relationships"`
		Effects []struct {
			Code   string `json:"code"`
			Count  int    `json:"count"`
			Effect string `json:"effect"`
		} `json:"affected_relationships"`
	} `json:"results"`
}

type bulkHostDeleteResponseView struct {
	IdempotencyKey string `json:"idempotency_key"`
	Complete       bool   `json:"complete"`
	Results        []struct {
		Index      int    `json:"index"`
		Identifier string `json:"identifier"`
		Status     string `json:"status"`
		HTTPStatus int    `json:"http_status"`
		HostID     int64  `json:"host_id"`
		Code       string `json:"code"`
	} `json:"results"`
}

func TestBulkHostDeletePreviewConfirmationRBACStalenessReplayAndAudit(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	requireBulkHostDeleteMigration(t, db)

	resource := handlers.NewHostsResource(db, handlers.NewAuthorizer(db))
	access := rbac.NewStore(db, testResourceTables)
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("bulk-delete-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("bulk-delete-user-%d", uniq))
	user := middleware.UserContext{
		Kind: middleware.HumanPrincipal, UserID: userID,
		Username: fmt.Sprintf("bulk-delete-user-%d", uniq),
	}
	allowedInventoryID := insertInventory(t, db, orgID, fmt.Sprintf("bulk-delete-allowed-%d", uniq))
	deniedInventoryID := insertInventory(t, db, orgID, fmt.Sprintf("bulk-delete-denied-%d", uniq))
	grantObjectRole(t, access, rbac.Inventory, allowedInventoryID, rbac.AdminRole, userID)
	readyID := insertBulkDeleteHost(t, db, allowedInventoryID, "ready", false)
	runnerID := insertBulkDeleteHost(t, db, allowedInventoryID, "runner", true)
	delegatedID := insertBulkDeleteHost(t, db, allowedInventoryID, "delegated", false)
	staleID := insertBulkDeleteHost(t, db, allowedInventoryID, "stale", false)
	deniedID := insertBulkDeleteHost(t, db, deniedInventoryID, "denied", false)
	var retainedGroupID int64
	if err := db.Get(&retainedGroupID, `
		INSERT INTO groups (inventory_id,name) VALUES ($1,$2) RETURNING id`,
		allowedInventoryID, fmt.Sprintf("bulk-delete-retained-group-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO host_groups (host_id,group_id) VALUES ($1,$2)`, readyID, retainedGroupID); err != nil {
		t.Fatal(err)
	}
	var principalID, workflowID int64
	if err := db.Get(&principalID, `
		INSERT INTO service_principals (organization_id,name,created_by_user_id)
		VALUES ($1,$2,$3) RETURNING id`,
		orgID, fmt.Sprintf("bulk-delete-principal-%d", uniq), userID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&workflowID, `
		INSERT INTO workflow_templates (organization_id,name) VALUES ($1,$2) RETURNING id`,
		orgID, fmt.Sprintf("bulk-delete-workflow-%d", uniq)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO delegated_launch_grants (
		    organization_id,service_principal_id,workflow_template_id,inventory_id,
		    allowed_host_ids,expires_at,created_by_user_id
		) VALUES ($1,$2,$3,$4,$5,now()+interval '1 day',$6)`,
		orgID, principalID, workflowID, allowedInventoryID,
		pq.Array([]int64{delegatedID}), userID); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM activity_stream WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM bulk_host_delete_requests WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM bulk_host_delete_previews WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM hosts WHERE inventory_id IN ($1,$2)`, allowedInventoryID, deniedInventoryID)
		_, _ = db.Exec(`DELETE FROM inventories WHERE id IN ($1,$2)`, allowedInventoryID, deniedInventoryID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	previewBody := fmt.Sprintf(`{"items":[
		{"identifier":"ready","host_id":%d},
		{"identifier":"runner","host_id":%d},
		{"identifier":"delegated","host_id":%d},
		{"identifier":"stale","host_id":%d},
		{"identifier":"denied","host_id":%d},
		{"identifier":"unknown","host_id":9223372036854775000}
	]}`, readyID, runnerID, delegatedID, staleID, deniedID)
	previewRecorder := callBulkHostDeletePreview(resource, previewBody, user)
	if previewRecorder.Code != http.StatusCreated {
		t.Fatalf("preview: want 201, got %d (%s)", previewRecorder.Code, previewRecorder.Body)
	}
	var preview bulkHostDeletePreviewView
	decodeJSONRecorder(t, previewRecorder, &preview)
	if preview.ConfirmationToken == "" || len(preview.Results) != 6 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	for _, index := range []int{0, 3} {
		if preview.Results[index].Status != "ready" || preview.Results[index].HostID == 0 {
			t.Fatalf("ready item %d: %+v", index, preview.Results[index])
		}
	}
	if got := preview.Results[0]; len(got.Effects) != 1 ||
		got.Effects[0].Code != "group_membership" ||
		got.Effects[0].Count != 1 || got.Effects[0].Effect != "delete" {
		t.Fatalf("dependent membership impact missing: %+v", got)
	}
	if got := preview.Results[1]; got.Status != "blocked" || got.Code != "blocking_relationships" ||
		len(got.Blockers) != 1 || got.Blockers[0].Code != "inventory_runner" {
		t.Fatalf("runner blocker missing: %+v", got)
	}
	if got := preview.Results[2]; got.Status != "blocked" || got.Code != "blocking_relationships" ||
		len(got.Blockers) != 1 || got.Blockers[0].Code != "delegated_launch_grant" {
		t.Fatalf("delegated launch blocker missing: %+v", got)
	}
	for _, index := range []int{4, 5} {
		got := preview.Results[index]
		if got.Status != "rejected" || got.Code != "not_found_or_forbidden" ||
			got.HostID != 0 || got.Name != "" || got.InventoryID != 0 {
			t.Fatalf("item %d leaked existence or authorization: %+v", index, got)
		}
	}
	if strings.Contains(previewRecorder.Body.String(), "snapshot_hash") {
		t.Fatalf("preview leaked internal binding: %s", previewRecorder.Body)
	}

	if _, err := db.Exec(`UPDATE hosts SET modified_at=modified_at+interval '1 second' WHERE id=$1`, staleID); err != nil {
		t.Fatal(err)
	}
	confirmBody := fmt.Sprintf(`{"confirmation_token":%q}`, preview.ConfirmationToken)
	first := callBulkHostDeleteConfirm(resource, confirmBody, "delete-hosts-1", user)
	if first.Code != http.StatusMultiStatus {
		t.Fatalf("confirmed deletion: want 207, got %d (%s)", first.Code, first.Body)
	}
	var response bulkHostDeleteResponseView
	decodeJSONRecorder(t, first, &response)
	if !response.Complete || response.IdempotencyKey != "delete-hosts-1" || len(response.Results) != 6 {
		t.Fatalf("unexpected confirmed response: %+v", response)
	}
	if got := response.Results[0]; got.Status != "deleted" || got.HTTPStatus != http.StatusNoContent || got.HostID != readyID {
		t.Fatalf("ready deletion result: %+v", got)
	}
	if got := response.Results[1]; got.Status != "rejected" || got.Code != "blocking_relationships" {
		t.Fatalf("blocked deletion result: %+v", got)
	}
	if got := response.Results[2]; got.Status != "rejected" || got.Code != "blocking_relationships" {
		t.Fatalf("delegated deletion result: %+v", got)
	}
	if got := response.Results[3]; got.Status != "rejected" || got.Code != "stale_preview" {
		t.Fatalf("stale deletion result: %+v", got)
	}
	for _, index := range []int{4, 5} {
		if got := response.Results[index]; got.Status != "rejected" || got.Code != "not_found_or_forbidden" {
			t.Fatalf("denied deletion result %d: %+v", index, got)
		}
	}
	var readyCount, protectedCount int
	if err := db.Get(&readyCount, `SELECT count(*) FROM hosts WHERE id=$1`, readyID); err != nil || readyCount != 0 {
		t.Fatalf("ready host still present: count=%d err=%v", readyCount, err)
	}
	var retainedGroups int
	if err := db.Get(&retainedGroups, `SELECT count(*) FROM groups WHERE id=$1`, retainedGroupID); err != nil || retainedGroups != 1 {
		t.Fatalf("bulk deletion cascaded to unrelated group: count=%d err=%v", retainedGroups, err)
	}
	if err := db.Get(&protectedCount, `SELECT count(*) FROM hosts WHERE id IN ($1,$2,$3,$4)`, runnerID, delegatedID, staleID, deniedID); err != nil || protectedCount != 4 {
		t.Fatalf("protected hosts changed: count=%d err=%v", protectedCount, err)
	}
	var audits int
	if err := db.Get(&audits, `
		SELECT count(*) FROM activity_stream
		 WHERE user_id=$1 AND action='delete' AND resource_type='host'
		   AND resource_id=$2 AND organization_id=$3`,
		userID, readyID, orgID); err != nil || audits != 1 {
		t.Fatalf("deletion audit count: got=%d err=%v", audits, err)
	}

	replay := callBulkHostDeleteConfirm(resource, confirmBody, "delete-hosts-1", user)
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Fatalf("idempotent replay changed:\nfirst=%s\nreplay=%s", first.Body, replay.Body)
	}
	reuse := callBulkHostDeleteConfirm(resource, confirmBody, "delete-hosts-2", user)
	if reuse.Code != http.StatusConflict {
		t.Fatalf("token reuse with another key: want 409, got %d (%s)", reuse.Code, reuse.Body)
	}
}

func TestBulkHostDeleteRejectsExpiredPreviewAndRequestBounds(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	requireBulkHostDeleteMigration(t, db)
	resource := handlers.NewHostsResource(db, handlers.NewAuthorizer(db))
	uniq := time.Now().UnixNano()
	userID := createUser(t, db, fmt.Sprintf("bulk-delete-bounds-%d", uniq))
	user := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: userID, Username: "bulk-delete-bounds"}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM bulk_host_delete_requests WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM bulk_host_delete_previews WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	items := make([]string, 101)
	for index := range items {
		items[index] = fmt.Sprintf(`{"host_id":%d}`, index+1)
	}
	oversized := callBulkHostDeletePreview(resource, `{"items":[`+strings.Join(items, ",")+`]}`, user)
	if oversized.Code != http.StatusBadRequest {
		t.Fatalf("oversized batch: want 400, got %d (%s)", oversized.Code, oversized.Body)
	}
	preview := callBulkHostDeletePreview(resource, `{"items":[{"host_id":9223372036854775000}]}`, user)
	if preview.Code != http.StatusCreated {
		t.Fatalf("bounded preview: want 201, got %d (%s)", preview.Code, preview.Body)
	}
	var view bulkHostDeletePreviewView
	decodeJSONRecorder(t, preview, &view)
	if _, err := db.Exec(`
		UPDATE bulk_host_delete_previews
		   SET created_at=now()-interval '10 minutes',
		       expires_at=now()-interval '1 second'
		 WHERE user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"confirmation_token":%q}`, view.ConfirmationToken)
	if rec := callBulkHostDeleteConfirm(resource, body, "expired-preview", user); rec.Code != http.StatusConflict {
		t.Fatalf("expired preview: want 409, got %d (%s)", rec.Code, rec.Body)
	}
	if rec := callBulkHostDeleteConfirm(resource, body, "", user); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key: want 400, got %d (%s)", rec.Code, rec.Body)
	}
}

func requireBulkHostDeleteMigration(t *testing.T, db interface {
	Get(interface{}, string, ...interface{}) error
}) {
	t.Helper()
	var exists bool
	if err := db.Get(&exists, `SELECT to_regclass('public.bulk_host_delete_previews') IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Skip("bulk host delete migration 000079 is not applied")
	}
}

func insertBulkDeleteHost(t *testing.T, db interface {
	Get(interface{}, string, ...interface{}) error
}, inventoryID int64, name string, runner bool) int64 {
	t.Helper()
	var id int64
	if err := db.Get(&id, `
		INSERT INTO hosts (inventory_id,name,variables,is_runner_host)
		VALUES ($1,$2,'{}'::jsonb,$3) RETURNING id`, inventoryID, name, runner); err != nil {
		t.Fatal(err)
	}
	return id
}

func callBulkHostDeletePreview(
	resource *handlers.HostsResource, body string, user middleware.UserContext,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bulk/hosts/delete/preview", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, user))
	rec := httptest.NewRecorder()
	resource.PreviewBulkDeleteHosts(rec, req)
	return rec
}

func callBulkHostDeleteConfirm(
	resource *handlers.HostsResource, body, key string, user middleware.UserContext,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bulk/hosts/delete", strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, user))
	rec := httptest.NewRecorder()
	resource.BulkDeleteHosts(rec, req)
	return rec
}

func decodeJSONRecorder(t *testing.T, recorder *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v (%s)", err, recorder.Body)
	}
}
