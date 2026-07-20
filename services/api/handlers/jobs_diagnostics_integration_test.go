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

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

func TestRunDiagnosticsPaginationAndRBAC(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	access := rbac.NewStore(db, testResourceTables)
	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("diagnostics-%d", uniq))
	readerID := createUser(t, db, fmt.Sprintf("diagnostics-reader-%d", uniq))
	deniedID := createUser(t, db, fmt.Sprintf("diagnostics-denied-%d", uniq))

	var inventoryID, unifiedTemplateID, templateID, jobID int64
	mustScan(t, db, `INSERT INTO inventories (organization_id,name) VALUES ($1,$2) RETURNING id`, &inventoryID, orgID, fmt.Sprintf("inv-%d", uniq))
	mustScan(t, db, `INSERT INTO unified_job_templates (name) VALUES ($1) RETURNING id`, &unifiedTemplateID, fmt.Sprintf("template-%d", uniq))
	mustScan(t, db, `INSERT INTO job_templates (organization_id,name,inventory_id,playbook,unified_job_template_id) VALUES ($1,$2,$3,'site.yml',$4) RETURNING id`, &templateID, orgID, fmt.Sprintf("template-%d", uniq), inventoryID, unifiedTemplateID)
	mustScan(t, db, `INSERT INTO unified_jobs (unified_job_template_id,name,status) VALUES ($1,$2,'running') RETURNING id`, &jobID, unifiedTemplateID, fmt.Sprintf("job-%d", uniq))
	runID := uuid.New()
	if _, err := db.Exec(`INSERT INTO execution_runs (id,unified_job_id,state,last_event_seq) VALUES ($1,$2,'running',5)`, runID, jobID); err != nil {
		t.Fatal(err)
	}
	for seq, outcome := range []string{"ok", "changed", "failed", "skipped", "unreachable"} {
		eventType := map[string]string{"ok": "HOST_OK", "changed": "HOST_CHANGED", "failed": "HOST_FAILED", "skipped": "HOST_SKIPPED", "unreachable": "HOST_UNREACHABLE"}[outcome]
		data, _ := json.Marshal(map[string]interface{}{"outcome": outcome, "failure_code": map[bool]string{true: "task_failed"}[outcome == "failed"]})
		if _, err := db.Exec(`INSERT INTO job_events (unified_job_id,execution_run_id,seq,event_type,event_data,created_at) VALUES ($1,$2,$3,$4,$5,now())`, jobID, runID, seq+1, eventType, data); err != nil {
			t.Fatal(err)
		}
	}
	grantObjectRole(t, access, rbac.JobTemplate, templateID, rbac.ReadRole, readerID)
	grantObjectRole(t, access, rbac.Inventory, inventoryID, rbac.ReadRole, readerID)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM unified_jobs WHERE id=$1`, jobID)
		_, _ = db.Exec(`DELETE FROM job_templates WHERE id=$1`, templateID)
		_, _ = db.Exec(`DELETE FROM unified_job_templates WHERE id=$1`, unifiedTemplateID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, readerID, deniedID)
	})

	resource := handlers.NewJobsResource(db, "", "", handlers.NewAuthorizer(db))
	first := callDiagnostics(t, resource, runID, "?kind=host&limit=2", readerID)
	if len(first.Events) != 2 || first.Events[0].Seq != 1 || first.Events[1].Seq != 2 || first.NextCursor == nil || *first.NextCursor != 2 {
		t.Fatalf("unstable first page: %#v", first)
	}
	second := callDiagnostics(t, resource, runID, "?kind=host&limit=2&cursor=2", readerID)
	if len(second.Events) != 2 || second.Events[0].Seq != 3 || second.Events[1].Seq != 4 {
		t.Fatalf("unstable second page: %#v", second)
	}

	req := httptest.NewRequest(http.MethodGet, "/runs/"+runID.String()+"/diagnostics", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, middleware.UserContext{UserID: deniedID}))
	rec := httptest.NewRecorder()
	resource.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized diagnostics status=%d body=%s", rec.Code, rec.Body)
	}

	// A live request must release promptly when its client disconnects.
	streamCtx, cancelStream := context.WithCancel(context.Background())
	streamReq := httptest.NewRequest(http.MethodGet, "/runs/"+runID.String()+"/diagnostics/stream", nil)
	streamReq = streamReq.WithContext(context.WithValue(streamCtx, middleware.UserContextKey, middleware.UserContext{UserID: readerID}))
	streamDone := make(chan struct{})
	go func() {
		resource.Routes().ServeHTTP(httptest.NewRecorder(), streamReq)
		close(streamDone)
	}()
	time.Sleep(50 * time.Millisecond)
	cancelStream()
	select {
	case <-streamDone:
	case <-time.After(time.Second):
		t.Fatal("diagnostic stream did not release after disconnect")
	}

	if _, err := db.Exec(`UPDATE execution_runs SET state='successful' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	streamReq = httptest.NewRequest(http.MethodGet, "/runs/"+runID.String()+"/diagnostics/stream?cursor=1", nil)
	streamReq.Header.Set("Last-Event-ID", "2") // header wins over the query cursor
	streamReq = streamReq.WithContext(context.WithValue(streamReq.Context(), middleware.UserContextKey, middleware.UserContext{UserID: readerID}))
	streamRec := httptest.NewRecorder()
	resource.Routes().ServeHTTP(streamRec, streamReq)
	if streamRec.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", streamRec.Code, streamRec.Body)
	}
	body := streamRec.Body.String()
	if strings.Contains(body, "id: 1\n") || strings.Contains(body, "id: 2\n") {
		t.Fatalf("stream replayed acknowledged events: %s", body)
	}
	for _, expected := range []string{"id: 3\n", "id: 4\n", "id: 5\n", "event: terminal", `"state":"successful"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("stream missing %q: %s", expected, body)
		}
	}
	for _, forbidden := range []string{"stdout", "event_data", "credential", "secret"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("stream exposed forbidden field %q: %s", forbidden, body)
		}
	}

	streamReq = httptest.NewRequest(http.MethodGet, "/runs/"+runID.String()+"/diagnostics/stream", nil)
	streamReq = streamReq.WithContext(context.WithValue(streamReq.Context(), middleware.UserContextKey, middleware.UserContext{UserID: deniedID}))
	streamRec = httptest.NewRecorder()
	resource.Routes().ServeHTTP(streamRec, streamReq)
	if streamRec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized stream status=%d body=%s", streamRec.Code, streamRec.Body)
	}
}

func callDiagnostics(t *testing.T, resource *handlers.JobsResource, runID uuid.UUID, query string, userID int64) dto.RunDiagnostics {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runs/"+runID.String()+"/diagnostics"+query, nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, middleware.UserContext{UserID: userID}))
	rec := httptest.NewRecorder()
	resource.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("diagnostics status=%d body=%s", rec.Code, rec.Body)
	}
	var response dto.RunDiagnostics
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func mustScan(t *testing.T, db *sqlx.DB, query string, destination interface{}, args ...interface{}) {
	t.Helper()
	if err := db.QueryRow(query, args...).Scan(destination); err != nil {
		t.Fatal(err)
	}
}
