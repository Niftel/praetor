package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/pkg/authorization"
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

// TestEffectiveCapabilitiesEndpoint proves the UI permission summary is sourced
// from the production decision point: administrators receive mutation
// capabilities while an auditor on the same organization remains read-only.
func TestEffectiveCapabilitiesEndpoint(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	resource := handlers.NewAccessResource(db, handlers.NewAuthorizer(db))
	access := rbac.NewStore(db, testResourceTables)

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("rbac-capabilities-%d", uniq))
	adminID := createUser(t, db, fmt.Sprintf("rbac-cap-admin-%d", uniq))
	auditorID := createUser(t, db, fmt.Sprintf("rbac-cap-auditor-%d", uniq))
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.AdminRole, adminID)
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.AuditorRole, auditorID)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, adminID, auditorID)
	})

	admin := effectiveCapabilities(t, resource, orgID, middleware.UserContext{UserID: adminID})
	if !admin["view"] || !admin["manage"] || !admin["add_inventory"] || !admin["add_workflow_template"] {
		t.Fatalf("organization administrator capabilities = %#v, want view/manage/add inventory/add workflow", admin)
	}
	auditor := effectiveCapabilities(t, resource, orgID, middleware.UserContext{UserID: auditorID})
	if !auditor["view"] || auditor["manage"] || auditor["add_inventory"] || auditor["add_workflow_template"] {
		t.Fatalf("organization auditor capabilities = %#v, want read-only", auditor)
	}
}

func effectiveCapabilities(t *testing.T, resource *handlers.AccessResource, objectID int64, user middleware.UserContext) map[string]bool {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/capabilities?content_type=organization&object_id=%d", objectID), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, user))
	rec := httptest.NewRecorder()
	resource.Capabilities(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities: status %d (%s)", rec.Code, rec.Body)
	}
	var result map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	return result
}

var testResourceTables = map[rbac.ResourceKind]string{
	rbac.Organization: "organizations", rbac.Team: "teams", rbac.Project: "projects",
	rbac.Inventory: "inventories", rbac.Credential: "credentials",
	rbac.JobTemplate: "job_templates", rbac.WorkflowTemplate: "workflow_templates",
}

// grantObjectRole grants a built-in role definition on an object for test setup.
func grantObjectRole(t *testing.T, access *rbac.Store, ct rbac.ResourceKind, objID int64, field rbac.RoleKind, userID int64) {
	t.Helper()
	name, ok := rbac.BuiltinRoleName(ct, field)
	if !ok {
		t.Fatalf("no built-in role for %s/%s", ct, field)
	}
	definition, err := access.RoleByName(context.Background(), name)
	if err != nil {
		t.Fatalf("find role %s: %v", name, err)
	}
	resource := rbac.Object(ct, objID)
	if err := access.Assign(context.Background(), rbac.Assignment{RoleDefinitionID: definition.ID, Resource: &resource, PrincipalKind: rbac.UserPrincipal, PrincipalID: userID}); err != nil {
		t.Fatalf("grant %s on %s/%d to user %d: %v", field, ct, objID, userID, err)
	}
}

// capCheck asks the production RBAC v4 decision adapter directly.
func capCheck(access *rbac.Store, user int64, ct rbac.ResourceKind, id int64, a rbac.Verb) (bool, error) {
	decision, err := authorization.NewPostgres(access.DB(), testResourceTables)
	if err != nil {
		return false, err
	}
	return decision.Can(context.Background(), rbac.Principal{UserID: user}, a, rbac.Object(ct, id))
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
	access := rbac.NewStore(db, testResourceTables)

	uniq := time.Now().UnixNano()
	orgA := createOrg(t, db, fmt.Sprintf("rbac-inv-orgA-%d", uniq))
	orgB := createOrg(t, db, fmt.Sprintf("rbac-inv-orgB-%d", uniq))
	admin := createUser(t, db, fmt.Sprintf("rbac-inv-admin-%d", uniq))
	reader := createUser(t, db, fmt.Sprintf("rbac-inv-reader-%d", uniq))
	nobody := createUser(t, db, fmt.Sprintf("rbac-inv-nobody-%d", uniq))
	grantObjectRole(t, access, rbac.Organization, orgA, rbac.AdminRole, admin)
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
	grantObjectRole(t, access, rbac.Inventory, invID, rbac.ReadRole, reader)

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
	jobsRes, err := handlers.NewJobsResource(db, "", "", handlers.NewAuthorizer(db))
	if err != nil {
		t.Fatal(err)
	}
	access := rbac.NewStore(db, testResourceTables)

	uniq := time.Now().UnixNano()
	orgA := createOrg(t, db, fmt.Sprintf("rbac-tmpl-org-%d", uniq))
	admin := createUser(t, db, fmt.Sprintf("rbac-tmpl-admin-%d", uniq))
	operator := createUser(t, db, fmt.Sprintf("rbac-tmpl-op-%d", uniq))
	nobody := createUser(t, db, fmt.Sprintf("rbac-tmpl-nobody-%d", uniq))
	grantObjectRole(t, access, rbac.Organization, orgA, rbac.AdminRole, admin)

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
	var inventoryID int64
	if err := db.QueryRow(
		`INSERT INTO inventories (organization_id, name) VALUES ($1,$2) RETURNING id`,
		orgA, fmt.Sprintf("rbac-tmpl-inventory-%d", uniq)).Scan(&inventoryID); err != nil {
		t.Fatalf("insert inventory: %v", err)
	}

	// Admin creates a template sourcing its playbook from the project.
	rec := callJSON(t, tmplRes.CreateTemplate, http.MethodPost,
		fmt.Sprintf(`{"name":"tmpl-%d","organization_id":%d,"project_id":%d,"inventory_id":%d,"playbook":"site.yml","ask_limit_on_launch":true}`, uniq, orgA, projID, inventoryID), adminUC, nil)
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
		_, _ = db.Exec(`DELETE FROM inventories WHERE id = $1`, inventoryID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id = $1`, orgA)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, admin, operator, nobody)
	})

	// operator gets execute on the template.
	grantObjectRole(t, access, rbac.JobTemplate, created.ID, rbac.ExecuteRole, operator)

	launchBody := fmt.Sprintf(`{"unified_job_template_id":%d,"name":"launch-%d","limit":"web-*"}`, *created.UnifiedJobTemplateID, uniq)

	// Template execute alone cannot use the attached inventory, even when the
	// client tries to narrow the launch with a limit.
	rec = callJSON(t, jobsRes.LaunchJob, http.MethodPost, launchBody, operatorUC, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator launch without inventory use: want 403, got %d (%s)", rec.Code, rec.Body)
	}
	grantObjectRole(t, access, rbac.Inventory, inventoryID, rbac.UseRole, operator)

	// Execute plus inventory use can launch, including a client-supplied limit.
	rec = callJSON(t, jobsRes.LaunchJob, http.MethodPost, launchBody, operatorUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("operator launch (execute): want 201, got %d (%s)", rec.Code, rec.Body)
	}

	// operator (execute, not admin) cannot edit the template.
	rec = callJSON(t, tmplRes.UpdateTemplate, http.MethodPut,
		fmt.Sprintf(`{"name":"tmpl-edited-%d","organization_id":%d,"project_id":%d,"inventory_id":%d,"playbook":"site.yml"}`, uniq, orgA, projID, inventoryID),
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
