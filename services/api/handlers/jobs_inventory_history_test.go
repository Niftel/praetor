package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

func TestJobsHistoryIncludesOnlyAuthorizedInventorySyncRuns(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	access := rbac.NewStore(db, testResourceTables)
	uniq := time.Now().UnixNano()

	orgID := createOrg(t, db, fmt.Sprintf("jobs-history-org-%d", uniq))
	inventoryReaderID := createUser(t, db, fmt.Sprintf("jobs-history-inventory-reader-%d", uniq))
	templateReaderID := createUser(t, db, fmt.Sprintf("jobs-history-template-reader-%d", uniq))
	deniedID := createUser(t, db, fmt.Sprintf("jobs-history-denied-%d", uniq))
	auditorID := createUser(t, db, fmt.Sprintf("jobs-history-auditor-%d", uniq))

	var inventoryID, otherInventoryID, sourceID, otherSourceID int64
	var unifiedTemplateID, templateID int64
	var templateJobID, syncJobID, otherSyncJobID int64
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM inventory_sync_history WHERE unified_job_id IN ($1,$2)`, syncJobID, otherSyncJobID)
		_, _ = db.Exec(`DELETE FROM unified_jobs WHERE id IN ($1,$2,$3)`, templateJobID, syncJobID, otherSyncJobID)
		_, _ = db.Exec(`DELETE FROM job_templates WHERE id=$1`, templateID)
		_, _ = db.Exec(`DELETE FROM unified_job_templates WHERE id=$1`, unifiedTemplateID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4)`, inventoryReaderID, templateReaderID, deniedID, auditorID)
	})
	mustScan(t, db, `INSERT INTO inventories (organization_id,name) VALUES ($1,$2) RETURNING id`,
		&inventoryID, orgID, fmt.Sprintf("jobs-history-inventory-%d", uniq))
	mustScan(t, db, `INSERT INTO inventories (organization_id,name) VALUES ($1,$2) RETURNING id`,
		&otherInventoryID, orgID, fmt.Sprintf("jobs-history-other-inventory-%d", uniq))
	mustScan(t, db, `INSERT INTO inventory_sources (inventory_id,name,source_kind,source) VALUES ($1,$2,'inventory','{}') RETURNING id`,
		&sourceID, inventoryID, fmt.Sprintf("jobs-history-source-%d", uniq))
	mustScan(t, db, `INSERT INTO inventory_sources (inventory_id,name,source_kind,source) VALUES ($1,$2,'inventory','{}') RETURNING id`,
		&otherSourceID, otherInventoryID, fmt.Sprintf("jobs-history-other-source-%d", uniq))

	mustScan(t, db, `INSERT INTO unified_job_templates (name) VALUES ($1) RETURNING id`,
		&unifiedTemplateID, fmt.Sprintf("jobs-history-template-%d", uniq))
	mustScan(t, db, `INSERT INTO job_templates (organization_id,name,playbook,unified_job_template_id) VALUES ($1,$2,'site.yml',$3) RETURNING id`,
		&templateID, orgID, fmt.Sprintf("jobs-history-template-%d", uniq), unifiedTemplateID)

	mustScan(t, db, `INSERT INTO unified_jobs (unified_job_template_id,name,status) VALUES ($1,$2,'successful') RETURNING id`,
		&templateJobID, unifiedTemplateID, fmt.Sprintf("jobs-history-template-job-%d", uniq))
	mustScan(t, db, `INSERT INTO unified_jobs (name,status,job_args) VALUES ($1,'successful',jsonb_build_object('inventory_source_id',$2::bigint)) RETURNING id`,
		&syncJobID, fmt.Sprintf("jobs-history-sync-job-%d", uniq), sourceID)
	mustScan(t, db, `INSERT INTO unified_jobs (name,status,job_args) VALUES ($1,'successful',jsonb_build_object('inventory_source_id',$2::bigint)) RETURNING id`,
		&otherSyncJobID, fmt.Sprintf("jobs-history-other-sync-job-%d", uniq), otherSourceID)

	grantObjectRole(t, access, rbac.Inventory, inventoryID, rbac.ReadRole, inventoryReaderID)
	grantObjectRole(t, access, rbac.JobTemplate, templateID, rbac.ReadRole, templateReaderID)
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

	resource, err := handlers.NewJobsResource(db, "", "", handlers.NewAuthorizer(db))
	if err != nil {
		t.Fatal(err)
	}
	inventoryReaderJobs := listJobs(t, resource, middleware.UserContext{UserID: inventoryReaderID})
	assertJobVisibility(t, inventoryReaderJobs, syncJobID, true)
	assertJobVisibility(t, inventoryReaderJobs, otherSyncJobID, false)
	assertJobVisibility(t, inventoryReaderJobs, templateJobID, false)

	templateReaderJobs := listJobs(t, resource, middleware.UserContext{UserID: templateReaderID})
	assertJobVisibility(t, templateReaderJobs, templateJobID, true)
	assertJobVisibility(t, templateReaderJobs, syncJobID, false)
	assertJobVisibility(t, templateReaderJobs, otherSyncJobID, false)

	deniedJobs := listJobs(t, resource, middleware.UserContext{UserID: deniedID})
	assertJobVisibility(t, deniedJobs, templateJobID, false)
	assertJobVisibility(t, deniedJobs, syncJobID, false)
	assertJobVisibility(t, deniedJobs, otherSyncJobID, false)

	auditorJobs := listJobs(t, resource, middleware.UserContext{UserID: auditorID})
	superuserJobs := listJobs(t, resource, middleware.UserContext{UserID: deniedID, IsSuperuser: true})
	for _, jobs := range [][]dto.UnifiedJob{auditorJobs, superuserJobs} {
		assertJobVisibility(t, jobs, templateJobID, true)
		assertJobVisibility(t, jobs, syncJobID, true)
		assertJobVisibility(t, jobs, otherSyncJobID, true)
	}
}

func listJobs(t *testing.T, resource *handlers.JobsResource, user middleware.UserContext) []dto.UnifiedJob {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/jobs/", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, user))
	rec := httptest.NewRecorder()
	resource.ListUnifiedJobs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list jobs: status %d (%s)", rec.Code, rec.Body)
	}
	var jobs []dto.UnifiedJob
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	return jobs
}

func assertJobVisibility(t *testing.T, jobs []dto.UnifiedJob, jobID int64, want bool) {
	t.Helper()
	for _, job := range jobs {
		if job.ID == jobID {
			if !want {
				t.Fatalf("job %d was visible outside its authorized scope", jobID)
			}
			return
		}
	}
	if want {
		t.Fatalf("authorized job %d was missing from Jobs history", jobID)
	}
}
