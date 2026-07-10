package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/pkg/launch"
)

// TestWorkflowNotificationsFire proves the scheduler dispatches a workflow
// template's attached notifications: 'error' when the run finalizes failed, and
// 'approval' when an approval node starts waiting. It stands up a real HTTP
// receiver (the webhook backend) and asserts the delivered body carries the
// workflow kind + event.
//
// Requires TEST_DATABASE_URL (migrated); skips otherwise.
func TestWorkflowNotificationsFire(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping workflow notification integration test")
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("cannot reach TEST_DATABASE_URL: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	sched := NewScheduler(db, time.Second, nil)
	uniq := time.Now().UnixNano()

	// HTTP receiver for the webhook backend; each delivery is pushed to got.
	got := make(chan map[string]interface{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		got <- m
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// --- Fixture: org, a webhook notification template pointing at srv, a workflow
	// template with a single approval node, and both event attachments. ---
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`,
		fmt.Sprintf("wf-notif-org-%d", uniq)).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID) })

	// Config is stored as plaintext {"url":...}; DecryptConfig passes through values
	// that aren't ciphertext, so no crypto setup is needed in the test.
	cfg, _ := json.Marshal(map[string]string{"url": srv.URL})
	var ntID int64
	if err := db.QueryRow(
		`INSERT INTO notification_templates (organization_id, name, notification_type, config)
		 VALUES ($1,$2,'webhook',$3) RETURNING id`,
		orgID, fmt.Sprintf("wf-notif-nt-%d", uniq), cfg).Scan(&ntID); err != nil {
		t.Fatalf("insert notification_template: %v", err)
	}

	var wtID int64
	if err := db.QueryRow(`INSERT INTO workflow_templates (organization_id, name) VALUES ($1,$2) RETURNING id`,
		orgID, fmt.Sprintf("wf-notif-wt-%d", uniq)).Scan(&wtID); err != nil {
		t.Fatalf("insert workflow_template: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO workflow_nodes (workflow_template_id, node_key, node_type, name)
		 VALUES ($1,'gate','approval','the gate')`, wtID); err != nil {
		t.Fatalf("insert approval node: %v", err)
	}
	for _, ev := range []string{"error", "approval"} {
		if _, err := db.Exec(
			`INSERT INTO workflow_template_notifications (workflow_template_id, notification_template_id, event)
			 VALUES ($1,$2,$3)`, wtID, ntID, ev); err != nil {
			t.Fatalf("attach %s notification: %v", ev, err)
		}
	}

	waitFor := func(t *testing.T, wantEvent, wantKind string) {
		t.Helper()
		for {
			select {
			case m := <-got:
				if m["event"] == wantEvent {
					if m["kind"] != wantKind {
						t.Fatalf("delivered %s: kind=%v, want %v", wantEvent, m["kind"], wantKind)
					}
					return
				}
				// A different event (e.g. approval arriving before error); keep waiting.
			case <-time.After(3 * time.Second):
				t.Fatalf("timed out waiting for %q notification delivery", wantEvent)
			}
		}
	}

	// --- 'approval' fires when the approval node starts waiting. ---
	wjID, err := launch.Workflow(ctx, db, wtID, launch.Options{})
	if err != nil {
		t.Fatalf("launch.Workflow: %v", err)
	}
	if err := sched.advanceWorkflow(ctx, wjID); err != nil {
		t.Fatalf("advanceWorkflow (approval): %v", err)
	}
	waitFor(t, "approval", "workflow approval")

	// --- 'error' fires when the run finalizes failed. Reject the waiting node, then
	// advance: all nodes terminal, one rejected -> workflow failed. ---
	if _, err := db.Exec(`UPDATE workflow_job_nodes SET status='rejected' WHERE workflow_job_id=$1 AND node_key='gate'`, wjID); err != nil {
		t.Fatalf("reject node: %v", err)
	}
	if err := sched.advanceWorkflow(ctx, wjID); err != nil {
		t.Fatalf("advanceWorkflow (finalize): %v", err)
	}
	waitFor(t, "error", "workflow")

	// The run must be recorded failed.
	var status string
	if err := db.Get(&status, `SELECT status FROM workflow_jobs WHERE id=$1`, wjID); err != nil {
		t.Fatalf("read workflow status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("workflow status = %q, want failed", status)
	}
}
