package store

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/rbac"
)

// contentTypeTable whitelists the physical table for each RBAC content type, so
// AllIDsOfType can build a query without risking injection.
var contentTypeTable = map[rbac.ContentType]string{
	rbac.ContentTypeOrganization:     "organizations",
	rbac.ContentTypeTeam:             "teams",
	rbac.ContentTypeProject:          "projects",
	rbac.ContentTypeInventory:        "inventories",
	rbac.ContentTypeCredential:       "credentials",
	rbac.ContentTypeJobTemplate:      "job_templates",
	rbac.ContentTypeWorkflowTemplate: "workflow_templates",
}

// AllIDsOfType returns every object id of a content type — the global-tier answer for
// superusers and system auditors, who can see everything.
func (s *CapabilityStore) AllIDsOfType(ctx context.Context, ct rbac.ContentType) ([]int64, error) {
	table, ok := contentTypeTable[ct]
	if !ok {
		return nil, wrap("CapabilityStore.AllIDsOfType", fmt.Errorf("unknown content type %q", ct))
	}
	ids := []int64{}
	err := s.db.SelectContext(ctx, &ids, `SELECT id FROM `+table+` ORDER BY id`)
	return ids, wrap("CapabilityStore.AllIDsOfType", err)
}

// This file holds the assignment + evaluation access path for the DAB capability model
// (Gitea #96): giving users/teams roles on objects, and answering capability checks
// against the unified global + scoped model.

// actorHolds is the SQL fragment (parameterised by $1 = user id) testing whether the
// current user holds the object_role aliased `orl` — directly, or via team membership.
const actorHolds = `(
	EXISTS (SELECT 1 FROM role_user_assignments ua WHERE ua.object_role_id = orl.id AND ua.user_id = $1)
	OR EXISTS (SELECT 1 FROM role_team_assignments ta
	           JOIN team_members tm ON tm.team_id = ta.team_id
	           WHERE ta.object_role_id = orl.id AND tm.user_id = $1)
)`

// HasCapability reports whether the user holds `codename` on (contentType, objectID),
// unifying two tiers: a GLOBAL object role whose definition grants the codename (system
// roles; no per-object rows), or a materialised evaluation row (scoped roles). The legacy
// is_superuser / is_system_auditor bypass still runs ahead of this during dual-run.
func (s *CapabilityStore) HasCapability(ctx context.Context, userID int64, contentType rbac.ContentType, objectID int64, codename string) (bool, error) {
	var ok bool
	err := s.db.GetContext(ctx, &ok, `
		SELECT EXISTS (
			-- global tier: a NULL-scoped object role whose definition grants the codename
			SELECT 1
			FROM object_roles orl
			JOIN role_definition_permissions rdp ON rdp.role_definition_id = orl.role_definition_id
			JOIN dab_permissions p ON p.id = rdp.permission_id
			WHERE orl.content_type IS NULL AND p.codename = $2 AND `+actorHolds+`
			UNION ALL
			-- scoped tier: a materialised evaluation row for this exact object
			SELECT 1
			FROM role_evaluations e
			JOIN object_roles orl ON orl.id = e.object_role_id
			WHERE e.content_type = $3 AND e.object_id = $4 AND e.codename = $2 AND `+actorHolds+`
		)`, userID, codename, string(contentType), objectID)
	return ok, wrap("CapabilityStore.HasCapability", err)
}

// HasGlobalCapability reports whether the user holds a global (system) role whose
// definition grants `codename` — the "see everything" tier for system roles.
func (s *CapabilityStore) HasGlobalCapability(ctx context.Context, userID int64, codename string) (bool, error) {
	var ok bool
	err := s.db.GetContext(ctx, &ok, `
		SELECT EXISTS (
			SELECT 1 FROM object_roles orl
			JOIN role_definition_permissions rdp ON rdp.role_definition_id = orl.role_definition_id
			JOIN dab_permissions p ON p.id = rdp.permission_id
			WHERE orl.content_type IS NULL AND p.codename = $2 AND `+actorHolds+`
		)`, userID, codename)
	return ok, wrap("CapabilityStore.HasGlobalCapability", err)
}

// AccessibleIDs returns the object ids of contentType on which the user holds `codename`
// via the scoped tier (materialised rows). The global tier (system roles) grants every
// object and is handled by the flag bypass during dual-run, so it is not expanded here.
func (s *CapabilityStore) AccessibleIDs(ctx context.Context, userID int64, contentType rbac.ContentType, codename string) ([]int64, error) {
	ids := []int64{}
	err := s.db.SelectContext(ctx, &ids, `
		SELECT DISTINCT e.object_id
		FROM role_evaluations e
		JOIN object_roles orl ON orl.id = e.object_role_id
		WHERE e.content_type = $2 AND e.codename = $3 AND `+actorHolds+`
		ORDER BY e.object_id`, userID, string(contentType), codename)
	return ids, wrap("CapabilityStore.AccessibleIDs", err)
}

// EnsureObjectRole returns the id of the object_role for (definition, scope), creating it
// if absent. A nil contentType/objectID pair denotes a global (system) role. Runs in the
// given tx so it composes with assignment.
func ensureObjectRole(ctx context.Context, tx *sqlx.Tx, defID int64, contentType *string, objectID *int64) (int64, error) {
	var id int64
	// Global vs scoped are distinguished so the NULL-scope lookup matches correctly
	// (NULL = NULL is never true in a plain equality).
	if contentType == nil {
		err := tx.GetContext(ctx, &id,
			`SELECT id FROM object_roles WHERE role_definition_id = $1 AND content_type IS NULL`, defID)
		if err == nil {
			return id, nil
		}
		err = tx.GetContext(ctx, &id,
			`INSERT INTO object_roles (role_definition_id, content_type, object_id) VALUES ($1, NULL, NULL) RETURNING id`, defID)
		return id, err
	}
	err := tx.GetContext(ctx, &id,
		`SELECT id FROM object_roles WHERE role_definition_id = $1 AND content_type = $2 AND object_id = $3`,
		defID, *contentType, *objectID)
	if err == nil {
		return id, nil
	}
	err = tx.GetContext(ctx, &id,
		`INSERT INTO object_roles (role_definition_id, content_type, object_id) VALUES ($1, $2, $3) RETURNING id`,
		defID, *contentType, *objectID)
	return id, err
}

// GiveUserPermission assigns a user the role definition scoped to (contentType, objectID)
// — nil/nil for a global role — and refreshes the evaluation cache. Idempotent.
func (s *CapabilityStore) GiveUserPermission(ctx context.Context, defID int64, contentType *string, objectID *int64, userID int64) error {
	err := runInTx(ctx, s.db, func(tx *sqlx.Tx) error {
		orID, err := ensureObjectRole(ctx, tx, defID, contentType, objectID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO role_user_assignments (role_definition_id, user_id, object_role_id)
			VALUES ($1, $2, $3) ON CONFLICT (user_id, object_role_id) DO NOTHING`, defID, userID, orID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `SELECT rebuild_object_role_evaluations($1)`, orID)
		return err
	})
	return wrap("CapabilityStore.GiveUserPermission", err)
}

// GiveTeamPermission is GiveUserPermission for a team.
func (s *CapabilityStore) GiveTeamPermission(ctx context.Context, defID int64, contentType *string, objectID *int64, teamID int64) error {
	err := runInTx(ctx, s.db, func(tx *sqlx.Tx) error {
		orID, err := ensureObjectRole(ctx, tx, defID, contentType, objectID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO role_team_assignments (role_definition_id, team_id, object_role_id)
			VALUES ($1, $2, $3) ON CONFLICT (team_id, object_role_id) DO NOTHING`, defID, teamID, orID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `SELECT rebuild_object_role_evaluations($1)`, orID)
		return err
	})
	return wrap("CapabilityStore.GiveTeamPermission", err)
}

// AssignableRoles returns the RoleDefinitions that can be granted on an object of the
// given content type: the managed roles scoped to that type, plus any custom (unscoped or
// matching) definitions.
func (s *CapabilityStore) AssignableRoles(ctx context.Context, contentType string) ([]rbac.RoleDefinition, error) {
	defs := []rbac.RoleDefinition{}
	err := s.db.SelectContext(ctx, &defs,
		`SELECT `+roleDefinitionCols+` FROM role_definitions
		 WHERE content_type = $1 OR (managed = false AND content_type IS NULL)
		 ORDER BY managed DESC, name`, contentType)
	return defs, wrap("CapabilityStore.AssignableRoles", err)
}

// RevokeUserPermission removes a user's assignment of a definition scoped to an object.
func (s *CapabilityStore) RevokeUserPermission(ctx context.Context, defID int64, contentType string, objectID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM role_user_assignments ua USING object_roles orl
		WHERE ua.object_role_id = orl.id AND ua.user_id = $1
		  AND orl.role_definition_id = $2 AND orl.content_type = $3 AND orl.object_id = $4`,
		userID, defID, contentType, objectID)
	return wrap("CapabilityStore.RevokeUserPermission", err)
}

// RevokeTeamPermission removes a team's assignment of a definition scoped to an object.
func (s *CapabilityStore) RevokeTeamPermission(ctx context.Context, defID int64, contentType string, objectID, teamID int64) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM role_team_assignments ta USING object_roles orl
		WHERE ta.object_role_id = orl.id AND ta.team_id = $1
		  AND orl.role_definition_id = $2 AND orl.content_type = $3 AND orl.object_id = $4`,
		teamID, defID, contentType, objectID)
	return wrap("CapabilityStore.RevokeTeamPermission", err)
}

// RebuildAllForDefinition refreshes the evaluation cache for every object_role of a
// definition — used after a custom role's permission set changes.
func (s *CapabilityStore) RebuildAllForDefinition(ctx context.Context, defID int64) error {
	_, err := s.db.ExecContext(ctx,
		`SELECT rebuild_object_role_evaluations(id) FROM object_roles WHERE role_definition_id = $1`, defID)
	return wrap("CapabilityStore.RebuildAllForDefinition", err)
}
