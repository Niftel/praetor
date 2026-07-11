package store

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/pkg/rbac"
)

// TestCapabilityEquivalence proves #95/#96's acceptance criterion: after backfilling legacy
// grants into the DAB capability model, capability evaluation matches the legacy hierarchy
// for every (user × object × action) it is checked on. Integration test — needs a migrated
// Postgres in DATABASE_URL; skipped otherwise.
//
//	DATABASE_URL=postgres://... go test ./services/api/store/ -run TestCapabilityEquivalence -v
//
// Covers organization / project / inventory / credential / team (org-propagated + object-
// scoped grants + negatives). Job/workflow templates use the identical propagation path.
func TestCapabilityEquivalence(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("set DATABASE_URL to a migrated Postgres to run the equivalence test")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	ac := rbac.NewAccessChecker(db)
	cs := NewCapabilityStore(db)

	// Unique suffix so repeated runs against the same scratch DB don't collide.
	var suffix int64
	if err := db.Get(&suffix, `SELECT COALESCE(MAX(id),0)+1 FROM organizations`); err != nil {
		t.Fatalf("suffix: %v", err)
	}
	name := func(base string) string { return fmt.Sprintf("%s-eqv-%d", base, suffix) }

	one := func(q string, args ...any) int64 {
		var id int64
		if err := db.Get(&id, q, args...); err != nil {
			t.Fatalf("fixture %q: %v", q, err)
		}
		return id
	}

	org := one(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name("Org"))
	proj := one(`INSERT INTO projects (organization_id, name, scm_type, scm_url) VALUES ($1,$2,'git','https://x') RETURNING id`, org, name("Proj"))
	inv := one(`INSERT INTO inventories (organization_id, name) VALUES ($1,$2) RETURNING id`, org, name("Inv"))
	credType := one(`SELECT id FROM credential_types WHERE name='Machine'`)
	cred := one(`INSERT INTO credentials (organization_id, credential_type_id, name) VALUES ($1,$2,$3) RETURNING id`, org, credType, name("Cred"))
	team := one(`INSERT INTO teams (organization_id, name) VALUES ($1,$2) RETURNING id`, org, name("Team"))

	mkUser := func(u string) int64 {
		return one(`INSERT INTO users (username, password_hash, email) VALUES ($1,'',$2) RETURNING id`, name(u), name(u)+"@x.io")
	}
	uOrgAdmin := mkUser("orgadmin")
	uOrgAuditor := mkUser("orgauditor")
	uProjUse := mkUser("projuse")
	uInvUpdate := mkUser("invupdate")
	uNobody := mkUser("nobody")

	// Legacy grants via the AccessChecker (role_members on the object's role).
	grant := func(userID int64, ct rbac.ContentType, oid int64, rf rbac.RoleField) {
		role, err := ac.GetObjectRole(ctx, ct, oid, rf)
		if err != nil {
			t.Fatalf("GetObjectRole(%s,%d,%s): %v", ct, oid, rf, err)
		}
		if err := ac.AddUserToRole(ctx, role.ID, userID); err != nil {
			t.Fatalf("AddUserToRole: %v", err)
		}
	}
	grant(uOrgAdmin, rbac.ContentTypeOrganization, org, rbac.RoleFieldAdmin)
	grant(uOrgAuditor, rbac.ContentTypeOrganization, org, rbac.RoleFieldAuditor)
	grant(uProjUse, rbac.ContentTypeProject, proj, rbac.RoleFieldUse)
	grant(uInvUpdate, rbac.ContentTypeInventory, inv, rbac.RoleFieldUpdate)

	// Translate every legacy grant into the capability model.
	if _, err := cs.BackfillFromLegacy(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// The actions authorize() checks, each paired with its legacy check and its codename.
	legacy := func(userID int64, ct rbac.ContentType, oid int64, action rbac.Action) bool {
		var ok bool
		var err error
		switch action {
		case rbac.ActionView:
			ok, err = ac.CanRead(ctx, userID, ct, oid)
		case rbac.ActionManage:
			ok, err = ac.CanAdmin(ctx, userID, ct, oid)
		case rbac.ActionUse:
			ok, err = ac.CanUse(ctx, userID, ct, oid)
		case rbac.ActionUpdate:
			ok, err = ac.HasObjectRole(ctx, userID, ct, oid, rbac.RoleFieldUpdate)
		default:
			t.Fatalf("no legacy check for action %s", action)
		}
		if err != nil {
			t.Fatalf("legacy %s check: %v", action, err)
		}
		return ok
	}

	type target struct {
		ct      rbac.ContentType
		id      int64
		actions []rbac.Action
	}
	targets := []target{
		{rbac.ContentTypeOrganization, org, []rbac.Action{rbac.ActionView, rbac.ActionManage}},
		{rbac.ContentTypeProject, proj, []rbac.Action{rbac.ActionView, rbac.ActionManage, rbac.ActionUse, rbac.ActionUpdate}},
		{rbac.ContentTypeInventory, inv, []rbac.Action{rbac.ActionView, rbac.ActionManage, rbac.ActionUse, rbac.ActionUpdate}},
		{rbac.ContentTypeCredential, cred, []rbac.Action{rbac.ActionView, rbac.ActionManage, rbac.ActionUse}},
		{rbac.ContentTypeTeam, team, []rbac.Action{rbac.ActionView, rbac.ActionManage}},
	}
	users := map[string]int64{
		"orgadmin": uOrgAdmin, "orgauditor": uOrgAuditor,
		"projuse": uProjUse, "invupdate": uInvUpdate, "nobody": uNobody,
	}

	mismatches := 0
	for uname, uid := range users {
		for _, tg := range targets {
			for _, action := range tg.actions {
				want := legacy(uid, tg.ct, tg.id, action)
				got, err := cs.HasCapability(ctx, uid, tg.ct, tg.id, rbac.Codename(tg.ct, action))
				if err != nil {
					t.Fatalf("HasCapability: %v", err)
				}
				if want != got {
					mismatches++
					t.Errorf("MISMATCH user=%s %s/%d %s: legacy=%v capability=%v", uname, tg.ct, tg.id, action, want, got)
				}
			}
		}
	}
	if mismatches == 0 {
		t.Logf("equivalence holds across %d users × targets", len(users))
	}
}
