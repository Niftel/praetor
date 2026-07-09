package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

// TestProjectRBAC validates the enforcement pattern end-to-end against a real,
// fully-migrated DB (so the AWX role-creation triggers fire): a non-superuser
// can only create a project in an org they administer, the creator becomes
// admin of the new project, list results are scoped to readable objects, and a
// superuser bypasses all of it.
func TestProjectRBAC(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping RBAC integration test")
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("cannot reach TEST_DATABASE_URL: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	h := handlers.NewProjectsResource(db)
	access := rbac.NewAccessChecker(db)

	uniq := time.Now().UnixNano()
	orgA := createOrg(t, db, fmt.Sprintf("rbac-orgA-%d", uniq))
	orgB := createOrg(t, db, fmt.Sprintf("rbac-orgB-%d", uniq))
	admin := createUser(t, db, fmt.Sprintf("rbac-admin-%d", uniq))   // org A admin
	nobody := createUser(t, db, fmt.Sprintf("rbac-nobody-%d", uniq)) // no roles

	// Make `admin` an administrator of org A only.
	roleA, err := access.GetObjectRole(ctx, rbac.ContentTypeOrganization, orgA, rbac.RoleFieldAdmin)
	if err != nil {
		t.Fatalf("org A admin_role (trigger should have created it): %v", err)
	}
	if err := access.AddUserToRole(ctx, roleA.ID, admin); err != nil {
		t.Fatalf("grant org A admin: %v", err)
	}

	adminUC := middleware.UserContext{UserID: admin, Username: "rbac-admin"}
	nobodyUC := middleware.UserContext{UserID: nobody, Username: "rbac-nobody"}
	superUC := middleware.UserContext{UserID: admin, IsSuperuser: true} // superuser flag bypasses

	// 1. Create a project in org A as its admin -> 201.
	rec := callJSON(t, h.CreateProject, http.MethodPost, projectBody(orgA, fmt.Sprintf("p-A-%d", uniq)), adminUC, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create in own org: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	projectA := extractID(t, rec.Body.String())

	// 2. Same admin cannot create in org B (no admin there) -> 403.
	rec = callJSON(t, h.CreateProject, http.MethodPost, projectBody(orgB, fmt.Sprintf("p-B-%d", uniq)), adminUC, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin create in foreign org: want 403, got %d", rec.Code)
	}

	// 3. Creator was granted admin on the new project (creator-grants-admin).
	canAdmin, err := access.CanAdmin(ctx, admin, rbac.ContentTypeProject, projectA)
	if err != nil || !canAdmin {
		t.Fatalf("creator should administer the project they made: canAdmin=%v err=%v", canAdmin, err)
	}

	// 4. A user with no roles is denied syncing it (auth check precedes any work).
	rec = callJSON(t, h.SyncProject, http.MethodPost, "", nobodyUC, map[string]string{"id": fmt.Sprint(projectA)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nobody sync: want 403, got %d", rec.Code)
	}

	// 5. List scoping: `nobody` sees no projects; superuser sees the new one.
	if got := listProjectCount(t, h, nobodyUC); got != 0 {
		t.Fatalf("nobody should see 0 projects, saw %d", got)
	}
	if got := listProjectCount(t, h, superUC); got < 1 {
		t.Fatalf("superuser should see project(s), saw %d", got)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id IN ($1,$2)`, orgA, orgB)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, admin, nobody)
	})
}

// --- helpers ---

type handlerFn func(http.ResponseWriter, *http.Request)

func callJSON(t *testing.T, fn handlerFn, method, body string, uc middleware.UserContext, urlParams map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	for k, v := range urlParams {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, middleware.UserContextKey, uc)
	rec := httptest.NewRecorder()
	fn(rec, req.WithContext(ctx))
	return rec
}

func projectBody(orgID int64, name string) string {
	return fmt.Sprintf(`{"name":%q,"scm_url":"https://example.invalid/repo.git","organization_id":%d}`, name, orgID)
}

func createOrg(t *testing.T, db *sqlx.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("create org: %v", err)
	}
	return id
}

func createUser(t *testing.T, db *sqlx.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`INSERT INTO users (username, password_hash, email, is_active) VALUES ($1, 'x', $2, TRUE) RETURNING id`,
		name, name+"@example.com",
	).Scan(&id); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func extractID(t *testing.T, body string) int64 {
	t.Helper()
	var id int64
	// Response is the created project JSON; pull "id":N without a full struct.
	if _, err := fmt.Sscanf(body[strings.Index(body, `"id":`):], `"id":%d`, &id); err != nil || id == 0 {
		t.Fatalf("could not extract project id from %q: %v", body, err)
	}
	return id
}

func listProjectCount(t *testing.T, h *handlers.ProjectsResource, uc middleware.UserContext) int {
	t.Helper()
	rec := callJSON(t, h.ListProjects, http.MethodGet, "", uc, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list projects: status %d", rec.Code)
	}
	// Count "id": occurrences inside the items array as a cheap proxy.
	return strings.Count(rec.Body.String(), `"scm_url"`)
}
