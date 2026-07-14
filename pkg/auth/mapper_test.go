package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/pkg/rbac"
)

// fakeResolver returns a canned identity so the mapper is testable without a live
// LDAP server.
type fakeResolver struct {
	id  *UserIdentity
	err error
}

func (f fakeResolver) AuthenticateAndResolve(_, _ string) (*UserIdentity, error) {
	return f.id, f.err
}

// mapperTestDB connects to TEST_DATABASE_URL (a migrated Praetor DB) or skips.
func mapperTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping mapper integration test")
	}
	db, err := sqlx.Connect("postgres", url)
	if err != nil {
		t.Skipf("cannot reach TEST_DATABASE_URL: %v", err)
	}
	return db
}

// objRoleMemberCount counts a user's capability assignment of the managed RoleDefinition
// mirroring a legacy (content_type, role_field) on an object.
func objRoleMemberCount(t *testing.T, db *sqlx.DB, ct string, objID, userID int64, field string) int {
	t.Helper()
	name, ok := rbac.ManagedNameForLegacy(rbac.ContentType(ct), rbac.RoleField(field))
	if !ok {
		t.Fatalf("no managed role for %s/%s", ct, field)
	}
	var n int
	err := db.Get(&n, `
		SELECT count(*) FROM role_user_assignments ua
		JOIN object_roles orl ON orl.id = ua.object_role_id
		JOIN role_definitions d ON d.id = ua.role_definition_id
		WHERE orl.content_type=$1 AND orl.object_id=$2 AND d.name=$3 AND ua.user_id=$4`,
		ct, objID, name, userID)
	if err != nil {
		t.Fatalf("assignment count: %v", err)
	}
	return n
}

// globalRoleCount counts a user's assignment of a global (system) RoleDefinition.
func globalRoleCount(t *testing.T, db *sqlx.DB, defName string, userID int64) int {
	t.Helper()
	var n int
	err := db.Get(&n, `
		SELECT count(*) FROM role_user_assignments ua
		JOIN object_roles orl ON orl.id = ua.object_role_id
		JOIN role_definitions d ON d.id = ua.role_definition_id
		WHERE d.name=$1 AND orl.content_type IS NULL AND ua.user_id=$2`, defName, userID)
	if err != nil {
		t.Fatalf("global role count: %v", err)
	}
	return n
}

// TestMapperRoleDefinitionBinding proves #98: an organization_map `roles:` entry binds a
// directory group to a DAB RoleDefinition by name, scoped to the org, with grant/revoke
// semantics — and an unknown role name is a hard error.
func TestMapperRoleDefinitionBinding(t *testing.T) {
	db := mapperTestDB(t)
	ctx := context.Background()
	sfx := fmt.Sprintf("%d", time.Now().UnixNano())

	orgName := "ldaprd-org-" + sfx
	userName := "ldaprd-user-" + sfx
	auditGroup := "cn=eng-audit-" + sfx + ",ou=teams,dc=x"

	t.Cleanup(func() {
		db.Exec(`DELETE FROM role_user_assignments WHERE user_id IN (SELECT id FROM users WHERE username=$1)`, userName)
		db.Exec(`DELETE FROM object_roles WHERE object_id IN (SELECT id FROM organizations WHERE name=$1)`, orgName)
		db.Exec(`DELETE FROM users WHERE username=$1`, userName)
		db.Exec(`DELETE FROM organizations WHERE name=$1`, orgName)
	})

	assignCount := func(defName string, orgID, userID int64) int {
		t.Helper()
		var n int
		if err := db.Get(&n, `
			SELECT count(*) FROM role_user_assignments ua
			JOIN object_roles orl ON orl.id = ua.object_role_id
			JOIN role_definitions d ON d.id = ua.role_definition_id
			WHERE d.name=$1 AND orl.content_type='organization' AND orl.object_id=$2 AND ua.user_id=$3`,
			defName, orgID, userID); err != nil {
			t.Fatalf("assign count: %v", err)
		}
		return n
	}

	cfg := &LDAPConfig{
		GroupType: LDAPGroupTypeConfig{Type: GroupTypeMemberDN, SearchBase: "ou=teams,dc=x"},
		OrganizationMap: map[string]LDAPOrgMapEntry{
			orgName: {
				Roles:       map[string]GroupMatch{"Organization Auditor": {DNs: []string{auditGroup}}},
				RemoveRoles: true,
			},
		},
	}

	// --- Login 1: in the audit group → the RoleDefinition is assigned. ---
	id := &UserIdentity{DN: "uid=" + userName + ",ou=users,dc=x", Username: userName,
		Groups: normalizeDNSet([]string{auditGroup})}
	u, err := Authenticate(ctx, db, cfg, fakeResolver{id: id}, userName, "pw")
	if err != nil {
		t.Fatalf("login 1: %v", err)
	}
	var orgID int64
	if err := db.Get(&orgID, `SELECT id FROM organizations WHERE name=$1`, orgName); err != nil {
		t.Fatalf("org not created: %v", err)
	}
	if assignCount("Organization Auditor", orgID, u.ID) != 1 {
		t.Error("expected Organization Auditor assigned on login 1")
	}
	// The assignment must actually confer a capability: view_organization on the org.
	var canView bool
	if err := db.Get(&canView, `
		SELECT EXISTS (SELECT 1 FROM role_evaluations e JOIN object_roles orl ON orl.id=e.object_role_id
		JOIN role_user_assignments ua ON ua.object_role_id=orl.id
		WHERE e.content_type='organization' AND e.object_id=$1 AND e.codename='view_organization' AND ua.user_id=$2)`,
		orgID, u.ID); err != nil {
		t.Fatalf("capability check: %v", err)
	}
	if !canView {
		t.Error("expected view_organization capability via the assigned RoleDefinition")
	}

	// --- Login 2: no groups + remove_roles → revoked. ---
	id2 := &UserIdentity{DN: id.DN, Username: userName, Groups: map[string]struct{}{}}
	if _, err := Authenticate(ctx, db, cfg, fakeResolver{id: id2}, userName, "pw"); err != nil {
		t.Fatalf("login 2: %v", err)
	}
	if assignCount("Organization Auditor", orgID, u.ID) != 0 {
		t.Error("expected Organization Auditor revoked on login 2 (remove_roles)")
	}

	// --- Unknown role name is a hard config error. ---
	bad := &LDAPConfig{
		OrganizationMap: map[string]LDAPOrgMapEntry{
			orgName: {Roles: map[string]GroupMatch{"No Such Role": {DNs: []string{auditGroup}}}},
		},
	}
	if _, err := Authenticate(ctx, db, bad, fakeResolver{id: &UserIdentity{
		DN: id.DN, Username: userName, Groups: normalizeDNSet([]string{auditGroup})}}, userName, "pw"); err == nil {
		t.Error("expected error for unknown role definition name")
	}
}

func TestMapperIntegration(t *testing.T) {
	db := mapperTestDB(t)
	ctx := context.Background()
	sfx := fmt.Sprintf("%d", time.Now().UnixNano())

	orgName := "ldaptest-org-" + sfx
	teamName := "ldaptest-team-" + sfx
	userName := "ldaptest-user-" + sfx
	localName := "ldaptest-local-" + sfx
	superGroup := "cn=supers-" + sfx + ",ou=teams,dc=x"
	adminGroup := "cn=eng-admins-" + sfx + ",ou=teams,dc=x"
	teamGroup := "cn=plat-" + sfx + ",ou=teams,dc=x"

	t.Cleanup(func() {
		db.Exec(`DELETE FROM role_user_assignments WHERE user_id IN (SELECT id FROM users WHERE username IN ($1,$2))`, userName, localName)
		db.Exec(`DELETE FROM users WHERE username IN ($1,$2)`, userName, localName)
		db.Exec(`DELETE FROM teams WHERE name=$1`, teamName)
		db.Exec(`DELETE FROM organizations WHERE name=$1`, orgName)
	})

	cfg := &LDAPConfig{
		GroupType: LDAPGroupTypeConfig{Type: GroupTypeMemberDN, SearchBase: "ou=teams,dc=x"},
		UserFlags: LDAPUserFlagsConfig{IsSuperuser: GroupDNList{DNs: []string{superGroup}}},
		OrganizationMap: map[string]LDAPOrgMapEntry{
			orgName: {Admins: GroupMatch{DNs: []string{adminGroup}}, RemoveAdmins: true},
		},
		TeamMap: map[string]LDAPTeamMapEntry{
			teamName: {Organization: orgName, Users: GroupMatch{DNs: []string{teamGroup}}, Remove: true},
		},
	}

	// --- Login 1: user is in all three groups. ---
	id := &UserIdentity{
		DN:       "uid=" + userName + ",ou=users,dc=x",
		Username: userName,
		Email:    "e@x.test",
		Groups:   normalizeDNSet([]string{superGroup, adminGroup, teamGroup}),
	}
	u, err := Authenticate(ctx, db, cfg, fakeResolver{id: id}, userName, "pw")
	if err != nil {
		t.Fatalf("login 1: %v", err)
	}
	if !u.IsSuperuser {
		t.Error("expected is_superuser true after group match")
	}
	if globalRoleCount(t, db, "System Administrator", u.ID) != 1 {
		t.Error("expected global System Administrator assignment after superuser grant")
	}

	var orgID, teamID int64
	if err := db.Get(&orgID, `SELECT id FROM organizations WHERE name=$1`, orgName); err != nil {
		t.Fatalf("org not created: %v", err)
	}
	if err := db.Get(&teamID, `SELECT id FROM teams WHERE name=$1 AND organization_id=$2`, teamName, orgID); err != nil {
		t.Fatalf("team not created: %v", err)
	}
	if objRoleMemberCount(t, db, "organization", orgID, u.ID, "admin_role") != 1 {
		t.Error("expected org admin_role granted")
	}
	if objRoleMemberCount(t, db, "team", teamID, u.ID, "member_role") != 1 {
		t.Error("expected team member_role granted")
	}

	// --- Login 2: user is now in NO groups → flag false + roles revoked. ---
	id2 := &UserIdentity{DN: id.DN, Username: userName, Groups: map[string]struct{}{}}
	u2, err := Authenticate(ctx, db, cfg, fakeResolver{id: id2}, userName, "pw")
	if err != nil {
		t.Fatalf("login 2: %v", err)
	}
	if u2.IsSuperuser {
		t.Error("expected is_superuser revoked to false (configured flag, no match)")
	}
	if globalRoleCount(t, db, "System Administrator", u.ID) != 0 {
		t.Error("expected global System Administrator assignment revoked on login 2")
	}
	if objRoleMemberCount(t, db, "organization", orgID, u.ID, "admin_role") != 0 {
		t.Error("expected org admin_role revoked (remove_admins)")
	}
	if objRoleMemberCount(t, db, "team", teamID, u.ID, "member_role") != 0 {
		t.Error("expected team member_role revoked (remove)")
	}

	// --- Break-glass: a local-password account is never taken over by LDAP. ---
	if _, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES ($1, 'localhash')`, localName); err != nil {
		t.Fatalf("seed local user: %v", err)
	}
	_, err = Authenticate(ctx, db, cfg, fakeResolver{id: &UserIdentity{
		DN: "uid=" + localName + ",ou=users,dc=x", Username: localName, Groups: map[string]struct{}{},
	}}, localName, "pw")
	if !errors.Is(err, ErrLocalAccount) {
		t.Errorf("expected ErrLocalAccount for local user, got %v", err)
	}
}
