package accesscontrol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

const (
	SystemAdministrator = "System Administrator"
	SystemAuditor       = "System Auditor"
)

// SetBuiltinAssignment writes one built-in role assignment through an existing
// database or transaction. This is the LDAP-safe authoring boundary.
func SetBuiltinAssignment(ctx context.Context, ext sqlx.ExtContext, resource Resource, role RoleKind, principal PrincipalKind, principalID int64, grant bool) error {
	name, ok := BuiltinRoleName(resource.Kind, role)
	if !ok {
		return fmt.Errorf("no built-in role for %s/%s", resource.Kind, role)
	}
	definitionID, err := roleDefinitionID(ctx, ext, name)
	if err != nil {
		return err
	}
	objectRoleID, err := scopedRoleID(ctx, ext, definitionID, &resource)
	if err != nil {
		return err
	}
	return setPrincipalAssignment(ctx, ext, definitionID, objectRoleID, principal, principalID, grant)
}

func SetGlobalUserRole(ctx context.Context, ext sqlx.ExtContext, roleName string, userID int64, grant bool) error {
	definitionID, err := roleDefinitionID(ctx, ext, roleName)
	if err != nil {
		return err
	}
	objectRoleID, err := scopedRoleID(ctx, ext, definitionID, nil)
	if err != nil {
		return err
	}
	return setPrincipalAssignment(ctx, ext, definitionID, objectRoleID, UserPrincipal, userID, grant)
}

func roleDefinitionID(ctx context.Context, ext sqlx.ExtContext, name string) (int64, error) {
	var id int64
	if err := sqlx.GetContext(ctx, ext, &id, `SELECT id FROM role_definitions WHERE name = $1`, name); err != nil {
		return 0, fmt.Errorf("find role definition %q: %w", name, err)
	}
	return id, nil
}

func scopedRoleID(ctx context.Context, ext sqlx.ExtContext, definitionID int64, resource *Resource) (int64, error) {
	var id int64
	if resource == nil {
		err := sqlx.GetContext(ctx, ext, &id, `SELECT id FROM object_roles WHERE role_definition_id = $1 AND content_type IS NULL`, definitionID)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		err = sqlx.GetContext(ctx, ext, &id, `INSERT INTO object_roles (role_definition_id) VALUES ($1) RETURNING id`, definitionID)
		return id, err
	}
	err := sqlx.GetContext(ctx, ext, &id, `SELECT id FROM object_roles
		WHERE role_definition_id = $1 AND content_type = $2 AND object_id = $3`, definitionID, string(resource.Kind), resource.ID)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = sqlx.GetContext(ctx, ext, &id, `INSERT INTO object_roles (role_definition_id, content_type, object_id)
		VALUES ($1, $2, $3) RETURNING id`, definitionID, string(resource.Kind), resource.ID)
	return id, err
}

func setPrincipalAssignment(ctx context.Context, ext sqlx.ExtContext, definitionID, objectRoleID int64, principal PrincipalKind, principalID int64, grant bool) error {
	table, column := assignmentTable(principal)
	if table == "" {
		return fmt.Errorf("unknown principal kind %q", principal)
	}
	if grant {
		_, err := ext.ExecContext(ctx, `INSERT INTO `+table+` (role_definition_id, `+column+`, object_role_id)
			VALUES ($1, $2, $3) ON CONFLICT (`+column+`, object_role_id) DO NOTHING`, definitionID, principalID, objectRoleID)
		if err != nil {
			return err
		}
		_, err = ext.ExecContext(ctx, `SELECT rebuild_object_role_evaluations($1)`, objectRoleID)
		return err
	}
	_, err := ext.ExecContext(ctx, `DELETE FROM `+table+` WHERE object_role_id = $1 AND `+column+` = $2`, objectRoleID, principalID)
	return err
}
