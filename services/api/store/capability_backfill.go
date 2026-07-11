package store

import (
	"context"

	"github.com/praetordev/praetor/pkg/rbac"
)

// BackfillFromLegacy translates every existing legacy grant into the DAB capability model
// (Gitea #96), so evaluation matches the legacy hierarchy during dual-run. For each legacy
// object/org role membership it creates the mirror managed definition's object_role +
// assignment; the is_superuser / is_system_auditor flags become global System Admin /
// Auditor assignments. Idempotent — GiveUser/TeamPermission upsert and rebuild — so it is
// safe to re-run. Returns the number of assignments applied.
func (s *CapabilityStore) BackfillFromLegacy(ctx context.Context) (int, error) {
	defs, err := s.ListRoleDefinitions(ctx)
	if err != nil {
		return 0, err
	}
	idByName := make(map[string]int64, len(defs))
	for _, d := range defs {
		idByName[d.Name] = d.ID
	}
	resolve := func(name string, ok bool) (int64, bool) {
		if !ok {
			return 0, false
		}
		id, present := idByName[name]
		return id, present
	}

	applied := 0

	// 1. Legacy per-object / per-org user grants.
	type legacyGrant struct {
		ActorID     int64  `db:"actor_id"`
		ContentType string `db:"content_type"`
		ObjectID    int64  `db:"object_id"`
		RoleField   string `db:"role_field"`
	}
	userGrants := []legacyGrant{}
	if err := s.db.SelectContext(ctx, &userGrants, `
		SELECT rm.user_id AS actor_id, r.content_type, r.object_id, r.role_field
		FROM role_members rm
		JOIN roles r ON r.id = rm.role_id
		WHERE r.content_type IS NOT NULL AND r.object_id IS NOT NULL`); err != nil {
		return applied, wrap("BackfillFromLegacy.userGrants", err)
	}
	for _, g := range userGrants {
		defID, ok := resolve(rbac.ManagedNameForLegacy(rbac.ContentType(g.ContentType), rbac.RoleField(g.RoleField)))
		if !ok {
			continue // e.g. notification_admin_role has no capability mirror yet
		}
		ct, oid := g.ContentType, g.ObjectID
		if err := s.GiveUserPermission(ctx, defID, &ct, &oid, g.ActorID); err != nil {
			return applied, err
		}
		applied++
	}

	// 2. Legacy per-object / per-org team grants.
	teamGrants := []legacyGrant{}
	if err := s.db.SelectContext(ctx, &teamGrants, `
		SELECT tr.team_id AS actor_id, r.content_type, r.object_id, r.role_field
		FROM team_roles tr
		JOIN roles r ON r.id = tr.role_id
		WHERE r.content_type IS NOT NULL AND r.object_id IS NOT NULL`); err != nil {
		return applied, wrap("BackfillFromLegacy.teamGrants", err)
	}
	for _, g := range teamGrants {
		defID, ok := resolve(rbac.ManagedNameForLegacy(rbac.ContentType(g.ContentType), rbac.RoleField(g.RoleField)))
		if !ok {
			continue
		}
		ct, oid := g.ContentType, g.ObjectID
		if err := s.GiveTeamPermission(ctx, defID, &ct, &oid, g.ActorID); err != nil {
			return applied, err
		}
		applied++
	}

	// 3. System roles from the user flags -> global object roles.
	if adminID, ok := resolve(rbac.ManagedNameForSingleton(rbac.SingletonSystemAdministrator)); ok {
		ids := []int64{}
		if err := s.db.SelectContext(ctx, &ids, `SELECT id FROM users WHERE is_superuser = true`); err != nil {
			return applied, wrap("BackfillFromLegacy.superusers", err)
		}
		for _, uid := range ids {
			if err := s.GiveUserPermission(ctx, adminID, nil, nil, uid); err != nil {
				return applied, err
			}
			applied++
		}
	}
	if auditorID, ok := resolve(rbac.ManagedNameForSingleton(rbac.SingletonSystemAuditor)); ok {
		ids := []int64{}
		if err := s.db.SelectContext(ctx, &ids,
			`SELECT id FROM users WHERE COALESCE(is_system_auditor, false) = true AND is_superuser = false`); err != nil {
			return applied, wrap("BackfillFromLegacy.auditors", err)
		}
		for _, uid := range ids {
			if err := s.GiveUserPermission(ctx, auditorID, nil, nil, uid); err != nil {
				return applied, err
			}
			applied++
		}
	}

	return applied, nil
}
