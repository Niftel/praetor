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

	"github.com/jmoiron/sqlx"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

type bulkHostResultView struct {
	Index      int    `json:"index"`
	Identifier string `json:"identifier"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status"`
	HostID     int64  `json:"host_id"`
	Code       string `json:"code"`
	Error      string `json:"error"`
}

type bulkHostResponseView struct {
	IdempotencyKey string               `json:"idempotency_key"`
	Complete       bool                 `json:"complete"`
	Results        []bulkHostResultView `json:"results"`
}

func TestBulkHostCreateMixedAuthorizationDuplicateReplayAndAudit(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	requireBulkHostMigration(t, db)

	resource := handlers.NewHostsResource(db, handlers.NewAuthorizer(db))
	access := rbac.NewStore(db, testResourceTables)
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("bulk-host-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("bulk-host-user-%d", uniq))
	user := middleware.UserContext{
		Kind: middleware.HumanPrincipal, UserID: userID,
		Username: fmt.Sprintf("bulk-host-user-%d", uniq),
	}
	allowedInventoryID := insertInventory(t, db, orgID, fmt.Sprintf("bulk-host-allowed-%d", uniq))
	deniedInventoryID := insertInventory(t, db, orgID, fmt.Sprintf("bulk-host-denied-%d", uniq))
	foreignOrgID := createOrg(t, db, fmt.Sprintf("bulk-host-foreign-org-%d", uniq))
	foreignInventoryID := insertInventory(t, db, foreignOrgID, fmt.Sprintf("bulk-host-foreign-%d", uniq))
	grantObjectRole(t, access, rbac.Inventory, allowedInventoryID, rbac.AdminRole, userID)
	if _, err := db.Exec(`
		INSERT INTO hosts (inventory_id,name,variables)
		VALUES ($1,'existing','{}'::jsonb)`, allowedInventoryID); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM activity_stream WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM bulk_host_create_requests WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM hosts WHERE inventory_id IN ($1,$2,$3)`, allowedInventoryID, deniedInventoryID, foreignInventoryID)
		_, _ = db.Exec(`DELETE FROM inventories WHERE id IN ($1,$2,$3)`, allowedInventoryID, deniedInventoryID, foreignInventoryID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id IN ($1,$2)`, orgID, foreignOrgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	body := fmt.Sprintf(`{"items":[
		{"identifier":"created","inventory_id":%d,"name":"app-01","description":"application","variables":{"ansible_host":"10.0.0.8"}},
		{"identifier":"duplicate","inventory_id":%d,"name":"existing"},
		{"identifier":"denied","inventory_id":%d,"name":"hidden"},
		{"identifier":"foreign","inventory_id":%d,"name":"hidden"},
		{"identifier":"unknown","inventory_id":9223372036854775000,"name":"hidden"}
	]}`, allowedInventoryID, allowedInventoryID, deniedInventoryID, foreignInventoryID)
	first := callBulkHostCreate(resource, body, "mixed-host-request", user)
	if first.Code != http.StatusMultiStatus {
		t.Fatalf("mixed bulk host create: want 207, got %d (%s)", first.Code, first.Body)
	}
	response := decodeBulkHostCreate(t, first)
	if !response.Complete || response.IdempotencyKey != "mixed-host-request" || len(response.Results) != 5 {
		t.Fatalf("unexpected bulk host response: %+v", response)
	}
	if got := response.Results[0]; got.Index != 0 || got.Identifier != "created" || got.Status != "created" || got.HostID == 0 {
		t.Fatalf("created host lost order or identity: %+v", got)
	}
	if got := response.Results[1]; got.Status != "rejected" || got.HTTPStatus != http.StatusConflict || got.Code != "duplicate" {
		t.Fatalf("duplicate host result: %+v", got)
	}
	for _, index := range []int{2, 3, 4} {
		got := response.Results[index]
		if got.Status != "rejected" || got.HTTPStatus != http.StatusForbidden ||
			got.Code != "not_found_or_forbidden" ||
			got.Error != "inventory not found or host creation not permitted" {
			t.Fatalf("item %d leaked inventory authorization/existence: %+v", index, got)
		}
	}

	var host struct {
		InventoryID int64           `db:"inventory_id"`
		Name        string          `db:"name"`
		Description string          `db:"description"`
		Variables   json.RawMessage `db:"variables"`
	}
	if err := db.Get(&host, `
		SELECT inventory_id,name,description,variables
		FROM hosts WHERE id=$1`, response.Results[0].HostID); err != nil {
		t.Fatal(err)
	}
	if host.InventoryID != allowedInventoryID || host.Name != "app-01" ||
		host.Description != "application" || !strings.Contains(string(host.Variables), `"ansible_host"`) {
		t.Fatalf("canonical host fields not preserved: %+v", host)
	}
	var audits int
	if err := db.Get(&audits, `
		SELECT count(*) FROM activity_stream
		WHERE user_id=$1 AND action='create' AND resource_type='host'
		  AND resource_id=$2 AND organization_id=$3`,
		userID, response.Results[0].HostID, orgID); err != nil || audits != 1 {
		t.Fatalf("created host audit count: got=%d err=%v", audits, err)
	}

	replay := callBulkHostCreate(resource, body, "mixed-host-request", user)
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Fatalf("idempotent replay changed response:\nfirst=%s\nreplay=%s", first.Body, replay.Body)
	}
	var count int
	if err := db.Get(&count, `
		SELECT count(*) FROM hosts WHERE inventory_id=$1 AND name='app-01'`,
		allowedInventoryID); err != nil || count != 1 {
		t.Fatalf("replay duplicated host: got=%d err=%v", count, err)
	}

	changed := fmt.Sprintf(`{"items":[{"inventory_id":%d,"name":"different"}]}`, allowedInventoryID)
	conflict := callBulkHostCreate(resource, changed, "mixed-host-request", user)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("changed request with reused key: want 409, got %d (%s)", conflict.Code, conflict.Body)
	}
}

func TestBulkHostCreateConcurrentReplayCreatesOneHost(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	requireBulkHostMigration(t, db)

	resource := handlers.NewHostsResource(db, handlers.NewAuthorizer(db))
	access := rbac.NewStore(db, testResourceTables)
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("bulk-host-concurrent-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("bulk-host-concurrent-user-%d", uniq))
	user := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: userID, Username: "bulk-host-concurrent"}
	inventoryID := insertInventory(t, db, orgID, fmt.Sprintf("bulk-host-concurrent-inventory-%d", uniq))
	grantObjectRole(t, access, rbac.Inventory, inventoryID, rbac.AdminRole, userID)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM activity_stream WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM bulk_host_create_requests WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM hosts WHERE inventory_id=$1`, inventoryID)
		_, _ = db.Exec(`DELETE FROM inventories WHERE id=$1`, inventoryID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	body := fmt.Sprintf(`{"items":[{"identifier":"only","inventory_id":%d,"name":"concurrent"}]}`, inventoryID)
	responses := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for index := range responses {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			responses[index] = callBulkHostCreate(resource, body, "concurrent-host-request", user)
		}(index)
	}
	wg.Wait()
	for index, response := range responses {
		if response.Code != http.StatusCreated {
			t.Fatalf("concurrent response %d: want 201, got %d (%s)", index, response.Code, response.Body)
		}
	}
	if responses[0].Body.String() != responses[1].Body.String() {
		t.Fatalf("concurrent replays differ: %s vs %s", responses[0].Body, responses[1].Body)
	}
	var count int
	if err := db.Get(&count, `
		SELECT count(*) FROM hosts WHERE inventory_id=$1 AND name='concurrent'`,
		inventoryID); err != nil || count != 1 {
		t.Fatalf("concurrent replay created %d hosts, err=%v", count, err)
	}
}

func TestBulkHostCreateRejectsMissingKeyAndBoundsBeforeWork(t *testing.T) {
	resource := &handlers.HostsResource{}
	user := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: 1, Username: "bounded"}
	valid := `{"items":[{"inventory_id":1,"name":"one"}]}`
	if rec := callBulkHostCreate(resource, valid, "", user); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key: want 400, got %d (%s)", rec.Code, rec.Body)
	}
	items := make([]string, 101)
	for index := range items {
		items[index] = `{"inventory_id":1,"name":"bounded"}`
	}
	oversized := `{"items":[` + strings.Join(items, ",") + `]}`
	if rec := callBulkHostCreate(resource, oversized, "bounded-request", user); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized host batch: want 400, got %d (%s)", rec.Code, rec.Body)
	}
	oversizedBody := `{"items":[{"inventory_id":1,"name":"` + strings.Repeat("x", 600<<10) + `"}]}`
	if rec := callBulkHostCreate(resource, oversizedBody, "bounded-body", user); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized host body: want 413, got %d (%s)", rec.Code, rec.Body)
	}
}

func requireBulkHostMigration(t *testing.T, db *sqlx.DB) {
	t.Helper()
	var exists bool
	if err := db.Get(&exists, `SELECT to_regclass('public.bulk_host_create_requests') IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Skip("bulk host migration 000078 is not applied")
	}
}

func callBulkHostCreate(
	resource *handlers.HostsResource,
	body, idempotencyKey string,
	user middleware.UserContext,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bulk/hosts/create", strings.NewReader(body))
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, user))
	rec := httptest.NewRecorder()
	resource.BulkCreateHosts(rec, req)
	return rec
}

func decodeBulkHostCreate(t *testing.T, recorder *httptest.ResponseRecorder) bulkHostResponseView {
	t.Helper()
	var response bulkHostResponseView
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode bulk host response: %v (%s)", err, recorder.Body)
	}
	return response
}
