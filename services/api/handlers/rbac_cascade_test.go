package handlers_test

import (
	"testing"
)

// TestRoleEdgeTablesCascade guards the orphan-edge fix (migration 000018). The
// role hierarchy/membership edge tables must each carry an ON DELETE CASCADE
// foreign key to roles on their role column(s), so deleting a role removes its
// own edges. Without these, deleted roles leave orphan edges that later collide
// with the unique constraints when the roles id sequence reaches a matching
// value, breaking object creation (POST /projects -> 500).
//
// This is a schema invariant rather than a behavioural test on purpose: the bug
// is reintroduced not by app-level deletes (which never delete roles) but by the
// migrator re-running 000011's `DROP TABLE roles CASCADE`, which drops these
// FKs — so what must be guaranteed is that the FKs are present after migration.
//
// Requires TEST_DATABASE_URL pointed at a DB migrated through 000018.
func TestRoleEdgeTablesCascade(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()

	cases := []struct{ table, column string }{
		{"role_parents", "role_id"},
		{"role_parents", "parent_role_id"},
		{"role_ancestors", "role_id"},
		{"role_ancestors", "ancestor_role_id"},
		{"role_members", "role_id"},
		{"team_roles", "role_id"},
	}

	for _, c := range cases {
		var hasCascadeFK bool
		err := db.Get(&hasCascadeFK, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_constraint con
				JOIN pg_attribute att
				  ON att.attrelid = con.conrelid AND att.attnum = ANY (con.conkey)
				WHERE con.conrelid = $1::regclass
				  AND con.contype = 'f'
				  AND con.confrelid = 'roles'::regclass
				  AND con.confdeltype = 'c'   -- 'c' = ON DELETE CASCADE
				  AND att.attname = $2
			)`, c.table, c.column)
		if err != nil {
			t.Fatalf("introspect %s.%s: %v", c.table, c.column, err)
		}
		if !hasCascadeFK {
			t.Errorf("%s.%s is missing an ON DELETE CASCADE foreign key to roles", c.table, c.column)
		}
	}
}
