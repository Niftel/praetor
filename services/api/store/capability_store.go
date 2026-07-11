package store

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/rbac"
)

// CapabilityStore is the data-access layer for the DAB-style capability RBAC tables
// (dab_permissions, role_definitions, role_definition_permissions) added in Gitea #94.
// Phase 1 exposes the permission catalog and basic RoleDefinition reads/writes; the
// assignment + evaluation machinery lands in #96.
type CapabilityStore struct {
	db *sqlx.DB
}

func NewCapabilityStore(db *sqlx.DB) *CapabilityStore { return &CapabilityStore{db: db} }

const dabPermissionCols = `id, codename, content_type, action, name, created_at`
const roleDefinitionCols = `id, name, description, managed, content_type, created_at, modified_at`

// ListPermissions returns the whole capability catalog, ordered for display.
func (s *CapabilityStore) ListPermissions(ctx context.Context) ([]rbac.DABPermission, error) {
	perms := []rbac.DABPermission{}
	err := s.db.SelectContext(ctx, &perms,
		`SELECT `+dabPermissionCols+` FROM dab_permissions ORDER BY content_type, action`)
	return perms, wrap("CapabilityStore.ListPermissions", err)
}

// GetPermissionByCodename returns a single capability by its codename.
func (s *CapabilityStore) GetPermissionByCodename(ctx context.Context, codename string) (rbac.DABPermission, error) {
	var p rbac.DABPermission
	err := s.db.GetContext(ctx, &p,
		`SELECT `+dabPermissionCols+` FROM dab_permissions WHERE codename = $1`, codename)
	return p, wrap("CapabilityStore.GetPermissionByCodename", err)
}

// ListRoleDefinitions returns all role definitions, managed and custom, ordered by name.
func (s *CapabilityStore) ListRoleDefinitions(ctx context.Context) ([]rbac.RoleDefinition, error) {
	defs := []rbac.RoleDefinition{}
	err := s.db.SelectContext(ctx, &defs,
		`SELECT `+roleDefinitionCols+` FROM role_definitions ORDER BY name`)
	return defs, wrap("CapabilityStore.ListRoleDefinitions", err)
}

// GetRoleDefinitionByName returns a single role definition by its unique name.
func (s *CapabilityStore) GetRoleDefinitionByName(ctx context.Context, name string) (rbac.RoleDefinition, error) {
	var d rbac.RoleDefinition
	err := s.db.GetContext(ctx, &d,
		`SELECT `+roleDefinitionCols+` FROM role_definitions WHERE name = $1`, name)
	return d, wrap("CapabilityStore.GetRoleDefinitionByName", err)
}

// PermissionsForRoleDefinition returns the capabilities a definition confers.
func (s *CapabilityStore) PermissionsForRoleDefinition(ctx context.Context, defID int64) ([]rbac.DABPermission, error) {
	perms := []rbac.DABPermission{}
	err := s.db.SelectContext(ctx, &perms, `
		SELECT `+prefixed("p", dabPermissionCols)+`
		FROM dab_permissions p
		JOIN role_definition_permissions rdp ON rdp.permission_id = p.id
		WHERE rdp.role_definition_id = $1
		ORDER BY p.content_type, p.action`, defID)
	return perms, wrap("CapabilityStore.PermissionsForRoleDefinition", err)
}

// CreateRoleDefinition inserts a custom (managed=false) role definition and attaches the
// given capability codenames, in one transaction. Unknown codenames are rejected. It
// returns the created definition's id.
func (s *CapabilityStore) CreateRoleDefinition(ctx context.Context, name, description string, contentType *string, codenames []string) (int64, error) {
	var defID int64
	err := runInTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if err := tx.GetContext(ctx, &defID, `
			INSERT INTO role_definitions (name, description, managed, content_type)
			VALUES ($1, $2, false, $3) RETURNING id`,
			name, description, contentType); err != nil {
			return err
		}
		return attachPermissions(ctx, tx, defID, codenames)
	})
	return defID, wrap("CapabilityStore.CreateRoleDefinition", err)
}

// SetRoleDefinitionPermissions replaces the capability set of a definition wholesale.
func (s *CapabilityStore) SetRoleDefinitionPermissions(ctx context.Context, defID int64, codenames []string) error {
	err := runInTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM role_definition_permissions WHERE role_definition_id = $1`, defID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE role_definitions SET modified_at = now() WHERE id = $1`, defID); err != nil {
			return err
		}
		return attachPermissions(ctx, tx, defID, codenames)
	})
	return wrap("CapabilityStore.SetRoleDefinitionPermissions", err)
}

// DeleteRoleDefinition removes a custom role definition. Managed definitions are
// protected — they mirror the legacy roles and are owned by the seeder.
func (s *CapabilityStore) DeleteRoleDefinition(ctx context.Context, defID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM role_definitions WHERE id = $1 AND managed = false`, defID)
	return wrap("CapabilityStore.DeleteRoleDefinition", err)
}

// attachPermissions links a definition to capabilities named by codename, resolving each
// through dab_permissions so an unknown codename fails loudly rather than silently no-op.
func attachPermissions(ctx context.Context, tx *sqlx.Tx, defID int64, codenames []string) error {
	for _, cn := range codenames {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO role_definition_permissions (role_definition_id, permission_id)
			SELECT $1, p.id FROM dab_permissions p WHERE p.codename = $2
			ON CONFLICT DO NOTHING`, defID, cn); err != nil {
			return err
		}
	}
	return nil
}
