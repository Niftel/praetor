package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

func rbacTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping RBAC integration test")
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("cannot reach TEST_DATABASE_URL: %v", err)
	}
	return db
}

// grantObjectRole grants the mirror capability RoleDefinition for a legacy field on an
// object (the capability model's assignment path), for test setup.
func grantObjectRole(t *testing.T, access *rbac.AccessChecker, ct rbac.ContentType, objID int64, field rbac.RoleField, userID int64) {
	t.Helper()
	if _, err := rbac.GrantCapabilityForLegacyFields(context.Background(), access.DB, string(ct), objID, string(field), userID, true); err != nil {
		t.Fatalf("grant %s on %s/%d to user %d: %v", field, ct, objID, userID, err)
	}
}

// capCheck answers a capability question with the same (bool, error) shape the legacy
// Can* checks had, so the RBAC tests read the same after the cutover.
func capCheck(access *rbac.AccessChecker, user int64, ct rbac.ContentType, id int64, a rbac.Action) (bool, error) {
	// HasCapability never touches the content-type→table map, so nil is fine here.
	return rbac.NewCapabilityStore(access.DB, nil).HasCapability(context.Background(), user, ct, id, rbac.Codename(ct, a))
}

// TestInventoryHostRBAC covers inventory create-scoping, the creator-admin
// grant, and host enforcement *derived from the parent inventory* (hosts have
// no roles of their own): read on the inventory lets you list hosts; admin is
// required to create them.
func TestInventoryHostRBAC(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	invRes := handlers.NewInventoriesResource(db, handlers.NewAuthorizer(db))
	hostRes := handlers.NewHostsResource(db, handlers.NewAuthorizer(db))
	access := rbac.NewAccessChecker(db)

	uniq := time.Now().UnixNano()
	orgA := createOrg(t, db, fmt.Sprintf("rbac-inv-orgA-%d", uniq))
	orgB := createOrg(t, db, fmt.Sprintf("rbac-inv-orgB-%d", uniq))
	admin := createUser(t, db, fmt.Sprintf("rbac-inv-admin-%d", uniq))
	reader := createUser(t, db, fmt.Sprintf("rbac-inv-reader-%d", uniq))
	nobody := createUser(t, db, fmt.Sprintf("rbac-inv-nobody-%d", uniq))
	grantObjectRole(t, access, rbac.ContentTypeOrganization, orgA, rbac.RoleFieldAdmin, admin)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id IN ($1,$2)`, orgA, orgB)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, admin, reader, nobody)
	})

	adminUC := middleware.UserContext{UserID: admin}
	readerUC := middleware.UserContext{UserID: reader}
	nobodyUC := middleware.UserContext{UserID: nobody}

	// Admin creates an inventory in org A; denied in org B.
	rec := callJSON(t, invRes.CreateInventory, http.MethodPost, fmt.Sprintf(`{"name":"inv-%d","organization_id":%d}`, uniq, orgA), adminUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create inventory in own org: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	invID := extractID(t, rec.Body.String())

	rec = callJSON(t, invRes.CreateInventory, http.MethodPost, fmt.Sprintf(`{"name":"inv-B-%d","organization_id":%d}`, uniq, orgB), adminUC, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin create inventory in foreign org: want 403, got %d", rec.Code)
	}

	// reader gets read on the inventory.
	grantObjectRole(t, access, rbac.ContentTypeInventory, invID, rbac.RoleFieldRead, reader)

	params := map[string]string{"inventoryId": fmt.Sprint(invID)}

	// Admin (creator -> admin) can create a host.
	rec = callJSON(t, hostRes.CreateHost, http.MethodPost, `{"name":"h1"}`, adminUC, params)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create host: want 201, got %d (%s)", rec.Code, rec.Body)
	}

	// reader can list hosts (read) but cannot create one (needs admin).
	rec = callJSON(t, hostRes.ListHosts, http.MethodGet, "", readerUC, params)
	if rec.Code != http.StatusOK {
		t.Fatalf("reader list hosts: want 200, got %d", rec.Code)
	}
	rec = callJSON(t, hostRes.CreateHost, http.MethodPost, `{"name":"h2"}`, readerUC, params)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("reader create host: want 403, got %d", rec.Code)
	}

	// nobody can't even list.
	rec = callJSON(t, hostRes.ListHosts, http.MethodGet, "", nobodyUC, params)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nobody list hosts: want 403, got %d", rec.Code)
	}
}

// TestTemplateExecuteRBAC covers the AWX verb distinction: execute lets you
// launch a template but not edit it; admin is needed to edit; no role can't
// launch.
func TestTemplateExecuteRBAC(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	tmplRes := handlers.NewTemplatesResource(db, handlers.NewAuthorizer(db))
	jobsRes := handlers.NewJobsResource(db, "", "", handlers.NewAuthorizer(db))
	access := rbac.NewAccessChecker(db)

	uniq := time.Now().UnixNano()
	orgA := createOrg(t, db, fmt.Sprintf("rbac-tmpl-org-%d", uniq))
	admin := createUser(t, db, fmt.Sprintf("rbac-tmpl-admin-%d", uniq))
	operator := createUser(t, db, fmt.Sprintf("rbac-tmpl-op-%d", uniq))
	nobody := createUser(t, db, fmt.Sprintf("rbac-tmpl-nobody-%d", uniq))
	grantObjectRole(t, access, rbac.ContentTypeOrganization, orgA, rbac.RoleFieldAdmin, admin)

	adminUC := middleware.UserContext{UserID: admin}
	operatorUC := middleware.UserContext{UserID: operator}
	nobodyUC := middleware.UserContext{UserID: nobody}

	// Inline playbooks are deprecated — a template must source its playbook from an
	// SCM project. Create one in orgA (the AFTER INSERT trigger grants its roles, so
	// the org admin inherits project use through the hierarchy).
	var projID int64
	if err := db.QueryRow(
		`INSERT INTO projects (organization_id, name, scm_type, scm_url) VALUES ($1,$2,'git','https://example.invalid/r.git') RETURNING id`,
		orgA, fmt.Sprintf("rbac-tmpl-proj-%d", uniq)).Scan(&projID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// Admin creates a template sourcing its playbook from the project.
	rec := callJSON(t, tmplRes.CreateTemplate, http.MethodPost,
		fmt.Sprintf(`{"name":"tmpl-%d","organization_id":%d,"project_id":%d,"playbook":"site.yml"}`, uniq, orgA, projID), adminUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create template: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	var created struct {
		ID                   int64  `json:"id"`
		UnifiedJobTemplateID *int64 `json:"unified_job_template_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 || created.UnifiedJobTemplateID == nil {
		t.Fatalf("parse created template: %v (%s)", err, rec.Body)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM unified_jobs WHERE unified_job_template_id = $1`, *created.UnifiedJobTemplateID)
		_, _ = db.Exec(`DELETE FROM unified_job_templates WHERE id = $1`, *created.UnifiedJobTemplateID)
		_, _ = db.Exec(`DELETE FROM projects WHERE id = $1`, projID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id = $1`, orgA)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, admin, operator, nobody)
	})

	// operator gets execute on the template.
	grantObjectRole(t, access, rbac.ContentTypeJobTemplate, created.ID, rbac.RoleFieldExecute, operator)

	launchBody := fmt.Sprintf(`{"unified_job_template_id":%d,"name":"launch-%d"}`, *created.UnifiedJobTemplateID, uniq)

	// operator (execute) can launch.
	rec = callJSON(t, jobsRes.LaunchJob, http.MethodPost, launchBody, operatorUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("operator launch (execute): want 201, got %d (%s)", rec.Code, rec.Body)
	}

	// operator (execute, not admin) cannot edit the template.
	rec = callJSON(t, tmplRes.UpdateTemplate, http.MethodPut,
		fmt.Sprintf(`{"name":"tmpl-edited-%d","organization_id":%d,"project_id":%d,"playbook":"site.yml"}`, uniq, orgA, projID),
		operatorUC, map[string]string{"id": fmt.Sprint(created.ID)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator edit template: want 403, got %d", rec.Code)
	}

	// nobody cannot launch.
	rec = callJSON(t, jobsRes.LaunchJob, http.MethodPost, launchBody, nobodyUC, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nobody launch: want 403, got %d", rec.Code)
	}
}
