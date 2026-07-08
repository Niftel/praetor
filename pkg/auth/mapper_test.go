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

func objRoleMemberCount(t *testing.T, db *sqlx.DB, ct string, objID, userID int64, field string) int {
	t.Helper()
	var n int
	err := db.Get(&n, `
		SELECT count(*) FROM role_members rm
		JOIN roles r ON r.id = rm.role_id
		WHERE r.content_type=$1 AND r.object_id=$2 AND r.role_field=$3 AND rm.user_id=$4`,
		ct, objID, field, userID)
	if err != nil {
		t.Fatalf("role count: %v", err)
	}
	return n
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
		db.Exec(`DELETE FROM role_members WHERE user_id IN (SELECT id FROM users WHERE username IN ($1,$2))`, userName, localName)
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
