package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

// TestUpdateTemplateRechecksUse proves UpdateTemplate re-checks use-on-reference
// for a credential the update attaches — a template admin without `use` on the
// new credential can't hijack it (parity with the create path).
func TestUpdateTemplateRechecksUse(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	rs := handlers.NewTemplatesResource(db)
	access := rbac.NewAccessChecker(db)
	ctx := context.Background()

	uniq := time.Now().UnixNano()
	org := createOrg(t, db, fmt.Sprintf("credscope-org-%d", uniq))

	var projID, credID, ujtID, tmplID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO projects (organization_id, name, scm_type, scm_url) VALUES ($1,$2,'git','https://example.invalid/r.git') RETURNING id`,
		org, fmt.Sprintf("p-%d", uniq)).Scan(&projID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO credentials (organization_id, credential_type_id, name) VALUES ($1, 1, $2) RETURNING id`,
		org, fmt.Sprintf("c-%d", uniq)).Scan(&credID); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO unified_job_templates (name) VALUES ($1) RETURNING id`, fmt.Sprintf("ujt-%d", uniq)).Scan(&ujtID); err != nil {
		t.Fatalf("insert ujt: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO job_templates (organization_id, name, playbook, unified_job_template_id, project_id)
		 VALUES ($1,$2,'site.yml',$3,$4) RETURNING id`,
		org, fmt.Sprintf("t-%d", uniq), ujtID, projID).Scan(&tmplID); err != nil {
		t.Fatalf("insert job_template: %v", err)
	}

	editor := createUser(t, db, fmt.Sprintf("credscope-editor-%d", uniq))
	grantObjectRole(t, access, rbac.ContentTypeJobTemplate, tmplID, rbac.RoleFieldAdmin, editor)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, org)
		_, _ = db.Exec(`DELETE FROM unified_job_templates WHERE id=$1`, ujtID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, editor)
	})

	editorUC := middleware.UserContext{UserID: editor}
	idParam := map[string]string{"id": fmt.Sprint(tmplID)}
	withCred := fmt.Sprintf(`{"organization_id":%d,"name":"t","project_id":%d,"playbook":"site.yml","credential_id":%d}`, org, projID, credID)

	// Template admin without use on the credential cannot attach it.
	rec := callJSON(t, rs.UpdateTemplate, http.MethodPut, withCred, editorUC, idParam)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("attach credential without use: want 403, got %d (%s)", rec.Code, rec.Body)
	}

	// Grant use on the credential -> the same update now succeeds.
	grantObjectRole(t, access, rbac.ContentTypeCredential, credID, rbac.RoleFieldUse, editor)
	rec = callJSON(t, rs.UpdateTemplate, http.MethodPut, withCred, editorUC, idParam)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach credential with use: want 200, got %d (%s)", rec.Code, rec.Body)
	}
}

// TestCredentialGrantOrgFence proves a credential admin cannot grant a role on
// the credential to a user who isn't a member of the credential's organization.
func TestCredentialGrantOrgFence(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	h := handlers.NewContentHandler(db)
	access := rbac.NewAccessChecker(db)
	ctx := context.Background()

	uniq := time.Now().UnixNano()
	org := createOrg(t, db, fmt.Sprintf("fence-org-%d", uniq))
	var credID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO credentials (organization_id, credential_type_id, name) VALUES ($1, 1, $2) RETURNING id`,
		org, fmt.Sprintf("fence-c-%d", uniq)).Scan(&credID); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	granter := createUser(t, db, fmt.Sprintf("fence-granter-%d", uniq))
	outsider := createUser(t, db, fmt.Sprintf("fence-outsider-%d", uniq))
	member := createUser(t, db, fmt.Sprintf("fence-member-%d", uniq))
	grantObjectRole(t, access, rbac.ContentTypeOrganization, org, rbac.RoleFieldCredentialAdmin, granter)
	grantObjectRole(t, access, rbac.ContentTypeOrganization, org, rbac.RoleFieldMember, member)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, org)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, granter, outsider, member)
	})

	useRole, err := access.GetObjectRole(ctx, rbac.ContentTypeCredential, credID, rbac.RoleFieldUse)
	if err != nil {
		t.Fatalf("credential use_role: %v", err)
	}
	granterUC := middleware.UserContext{UserID: granter}
	roleParam := map[string]string{"id": fmt.Sprint(useRole.ID)}

	// Granting use to a non-member is forbidden.
	rec := callJSON(t, h.AddRoleUser, http.MethodPost, fmt.Sprintf(`{"user_id":%d}`, outsider), granterUC, roleParam)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("grant to non-member: want 403, got %d (%s)", rec.Code, rec.Body)
	}
	// Granting use to an org member succeeds.
	rec = callJSON(t, h.AddRoleUser, http.MethodPost, fmt.Sprintf(`{"user_id":%d}`, member), granterUC, roleParam)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("grant to member: want 204, got %d (%s)", rec.Code, rec.Body)
	}
}
