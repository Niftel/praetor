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

type bulkLaunchResultView struct {
	Index      int    `json:"index"`
	Identifier string `json:"identifier"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status"`
	JobID      int64  `json:"job_id"`
	Code       string `json:"code"`
	Error      string `json:"error"`
}

type bulkLaunchResponseView struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	Complete       bool                   `json:"complete"`
	Results        []bulkLaunchResultView `json:"results"`
}

func TestBulkJobLaunchMixedAuthorizationReplayAndAudit(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	requireBulkLaunchMigration(t, db)

	resource, err := handlers.NewJobsResource(db, "", "", handlers.NewAuthorizer(db))
	if err != nil {
		t.Fatal(err)
	}
	access := rbac.NewStore(db, testResourceTables)
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("bulk-launch-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("bulk-launch-user-%d", uniq))
	user := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: userID, Username: fmt.Sprintf("bulk-launch-user-%d", uniq)}

	allowedInventoryID := insertInventory(t, db, orgID, fmt.Sprintf("bulk-allowed-inventory-%d", uniq))
	deniedInventoryID := insertInventory(t, db, orgID, fmt.Sprintf("bulk-denied-inventory-%d", uniq))
	allowedTemplateID, allowedUnifiedID := insertBulkTemplate(t, db, orgID, allowedInventoryID, fmt.Sprintf("bulk-allowed-template-%d", uniq))
	deniedTemplateID, deniedUnifiedID := insertBulkTemplate(t, db, orgID, deniedInventoryID, fmt.Sprintf("bulk-denied-template-%d", uniq))
	grantObjectRole(t, access, rbac.JobTemplate, allowedTemplateID, rbac.ExecuteRole, userID)
	grantObjectRole(t, access, rbac.Inventory, allowedInventoryID, rbac.UseRole, userID)
	grantObjectRole(t, access, rbac.JobTemplate, deniedTemplateID, rbac.ExecuteRole, userID)

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM activity_stream WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM bulk_job_launch_requests WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM unified_jobs WHERE unified_job_template_id IN ($1,$2)`, allowedUnifiedID, deniedUnifiedID)
		_, _ = db.Exec(`DELETE FROM job_templates WHERE id IN ($1,$2)`, allowedTemplateID, deniedTemplateID)
		_, _ = db.Exec(`DELETE FROM unified_job_templates WHERE id IN ($1,$2)`, allowedUnifiedID, deniedUnifiedID)
		_, _ = db.Exec(`DELETE FROM inventories WHERE id IN ($1,$2)`, allowedInventoryID, deniedInventoryID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	body := fmt.Sprintf(`{"items":[
		{"identifier":"allowed","unified_job_template_id":%d,"name":"accepted","extra_vars":{"release":"canary"},"limit":"web-*"},
		{"identifier":"inventory-denied","unified_job_template_id":%d,"name":"denied"},
		{"identifier":"unknown","unified_job_template_id":9223372036854775000,"name":"unknown"}
	]}`, allowedUnifiedID, deniedUnifiedID)
	first := callBulkLaunch(resource, body, "mixed-request", user)
	if first.Code != http.StatusMultiStatus {
		t.Fatalf("mixed bulk launch: want 207, got %d (%s)", first.Code, first.Body)
	}
	response := decodeBulkLaunch(t, first)
	if !response.Complete || response.IdempotencyKey != "mixed-request" || len(response.Results) != 3 {
		t.Fatalf("unexpected bulk response: %+v", response)
	}
	if got := response.Results[0]; got.Index != 0 || got.Identifier != "allowed" || got.Status != "accepted" || got.JobID == 0 {
		t.Fatalf("accepted item lost order or job identity: %+v", got)
	}
	for _, index := range []int{1, 2} {
		got := response.Results[index]
		if got.Index != index || got.Status != "rejected" || got.HTTPStatus != http.StatusForbidden ||
			got.Code != "not_found_or_forbidden" || got.Error != "job template not found or launch not permitted" {
			t.Fatalf("item %d leaked authorization/existence distinction: %+v", index, got)
		}
	}

	var jobs int
	if err := db.Get(&jobs, `SELECT count(*) FROM unified_jobs WHERE name='accepted' AND unified_job_template_id=$1`, allowedUnifiedID); err != nil || jobs != 1 {
		t.Fatalf("accepted launch count: got=%d err=%v", jobs, err)
	}
	var jobArgs string
	if err := db.Get(&jobArgs, `SELECT job_args::text FROM unified_jobs WHERE id=$1`, response.Results[0].JobID); err != nil ||
		!strings.Contains(jobArgs, `"release": "canary"`) || !strings.Contains(jobArgs, `"limit": "web-*"`) {
		t.Fatalf("bulk launch did not preserve canonical prompt handling: args=%s err=%v", jobArgs, err)
	}
	var audits int
	if err := db.Get(&audits, `
		SELECT count(*) FROM activity_stream
		WHERE user_id=$1 AND action='launch' AND resource_type='unified_job' AND resource_id=$2 AND organization_id=$3`,
		userID, response.Results[0].JobID, orgID); err != nil || audits != 1 {
		t.Fatalf("accepted launch audit count: got=%d err=%v", audits, err)
	}

	replay := callBulkLaunch(resource, body, "mixed-request", user)
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Fatalf("idempotent replay changed response:\nfirst=%s\nreplay=%s", first.Body, replay.Body)
	}
	if err := db.Get(&jobs, `SELECT count(*) FROM unified_jobs WHERE name='accepted' AND unified_job_template_id=$1`, allowedUnifiedID); err != nil || jobs != 1 {
		t.Fatalf("replay duplicated accepted launch: got=%d err=%v", jobs, err)
	}

	conflictBody := fmt.Sprintf(`{"items":[{"unified_job_template_id":%d,"name":"different"}]}`, allowedUnifiedID)
	conflict := callBulkLaunch(resource, conflictBody, "mixed-request", user)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("reused idempotency key with changed body: want 409, got %d (%s)", conflict.Code, conflict.Body)
	}
}

func TestBulkJobLaunchConcurrentReplayCreatesOneJob(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	requireBulkLaunchMigration(t, db)

	resource, err := handlers.NewJobsResource(db, "", "", handlers.NewAuthorizer(db))
	if err != nil {
		t.Fatal(err)
	}
	access := rbac.NewStore(db, testResourceTables)
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("bulk-concurrent-org-%d", uniq))
	userID := createUser(t, db, fmt.Sprintf("bulk-concurrent-user-%d", uniq))
	user := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: userID, Username: fmt.Sprintf("bulk-concurrent-user-%d", uniq)}
	inventoryID := insertInventory(t, db, orgID, fmt.Sprintf("bulk-concurrent-inventory-%d", uniq))
	templateID, unifiedID := insertBulkTemplate(t, db, orgID, inventoryID, fmt.Sprintf("bulk-concurrent-template-%d", uniq))
	grantObjectRole(t, access, rbac.JobTemplate, templateID, rbac.ExecuteRole, userID)
	grantObjectRole(t, access, rbac.Inventory, inventoryID, rbac.UseRole, userID)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM activity_stream WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM bulk_job_launch_requests WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM unified_jobs WHERE unified_job_template_id=$1`, unifiedID)
		_, _ = db.Exec(`DELETE FROM job_templates WHERE id=$1`, templateID)
		_, _ = db.Exec(`DELETE FROM unified_job_templates WHERE id=$1`, unifiedID)
		_, _ = db.Exec(`DELETE FROM inventories WHERE id=$1`, inventoryID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	body := fmt.Sprintf(`{"items":[{"identifier":"only","unified_job_template_id":%d,"name":"concurrent"}]}`, unifiedID)
	responses := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			responses[index] = callBulkLaunch(resource, body, "concurrent-request", user)
		}(i)
	}
	wg.Wait()
	for i, response := range responses {
		if response.Code != http.StatusCreated {
			t.Fatalf("concurrent response %d: want 201, got %d (%s)", i, response.Code, response.Body)
		}
	}
	if responses[0].Body.String() != responses[1].Body.String() {
		t.Fatalf("concurrent replays returned different results: %s vs %s", responses[0].Body, responses[1].Body)
	}
	var jobs int
	if err := db.Get(&jobs, `SELECT count(*) FROM unified_jobs WHERE name='concurrent' AND unified_job_template_id=$1`, unifiedID); err != nil || jobs != 1 {
		t.Fatalf("concurrent replay created %d jobs, err=%v", jobs, err)
	}
}

func TestBulkJobLaunchRejectsMissingKeyAndOversizedBatchBeforeWork(t *testing.T) {
	resource := &handlers.JobsResource{}
	user := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: 1, Username: "bounded"}
	valid := `{"items":[{"unified_job_template_id":1,"name":"one"}]}`
	if rec := callBulkLaunch(resource, valid, "", user); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key: want 400, got %d (%s)", rec.Code, rec.Body)
	}
	items := make([]string, 26)
	for i := range items {
		items[i] = `{"unified_job_template_id":1}`
	}
	oversized := `{"items":[` + strings.Join(items, ",") + `]}`
	if rec := callBulkLaunch(resource, oversized, "bounded-request", user); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized item batch: want 400, got %d (%s)", rec.Code, rec.Body)
	}
	oversizedBody := `{"items":[{"unified_job_template_id":1,"name":"` + strings.Repeat("x", 300<<10) + `"}]}`
	if rec := callBulkLaunch(resource, oversizedBody, "bounded-body", user); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request body: want 413, got %d (%s)", rec.Code, rec.Body)
	}
}

func requireBulkLaunchMigration(t *testing.T, db *sqlx.DB) {
	t.Helper()
	var exists bool
	if err := db.Get(&exists, `SELECT to_regclass('public.bulk_job_launch_requests') IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Skip("bulk job launch migration 000077 is not applied")
	}
}

func insertInventory(t *testing.T, db *sqlx.DB, orgID int64, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`INSERT INTO inventories (organization_id,name) VALUES ($1,$2) RETURNING id`, orgID, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertBulkTemplate(t *testing.T, db *sqlx.DB, orgID, inventoryID int64, name string) (int64, int64) {
	t.Helper()
	var unifiedID, templateID int64
	if err := db.QueryRow(`INSERT INTO unified_job_templates (name) VALUES ($1) RETURNING id`, name).Scan(&unifiedID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO job_templates (
		    organization_id,name,playbook,inventory_id,unified_job_template_id,
		    allow_simultaneous,ask_variables_on_launch,ask_limit_on_launch
		) VALUES ($1,$2,'site.yml',$3,$4,TRUE,TRUE,TRUE) RETURNING id`,
		orgID, name, inventoryID, unifiedID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	return templateID, unifiedID
}

func callBulkLaunch(resource *handlers.JobsResource, body, idempotencyKey string, user middleware.UserContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bulk/jobs/launch", strings.NewReader(body))
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, user))
	rec := httptest.NewRecorder()
	resource.BulkLaunchJobs(rec, req)
	return rec
}

func decodeBulkLaunch(t *testing.T, recorder *httptest.ResponseRecorder) bulkLaunchResponseView {
	t.Helper()
	var response bulkLaunchResponseView
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode bulk launch response: %v (%s)", err, recorder.Body)
	}
	return response
}
