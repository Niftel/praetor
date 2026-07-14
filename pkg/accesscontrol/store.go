package accesscontrol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type RoleDefinition struct {
	ID           int64   `db:"id" json:"id"`
	Name         string  `db:"name" json:"name"`
	Description  string  `db:"description" json:"description"`
	Managed      bool    `db:"managed" json:"managed"`
	ResourceKind *string `db:"content_type" json:"content_type,omitempty"`
}

type Store struct {
	db     *sqlx.DB
	tables map[ResourceKind]string
}

func NewStore(db *sqlx.DB, tables map[ResourceKind]string) *Store {
	return &Store{db: db, tables: tables}
}

func (s *Store) DB() *sqlx.DB { return s.db }

func (s *Store) OrganizationFor(ctx context.Context, resource Resource) (int64, bool) {
	if resource.Kind == Organization {
		return resource.ID, true
	}
	table, ok := s.tables[resource.Kind]
	if !ok {
		return 0, false
	}
	var organizationID int64
	if err := s.db.GetContext(ctx, &organizationID, "SELECT organization_id FROM "+table+" WHERE id = $1", resource.ID); err != nil {
		return 0, false
	}
	return organizationID, true
}

func (s *Store) RoleByName(ctx context.Context, name string) (RoleDefinition, error) {
	var role RoleDefinition
	err := s.db.GetContext(ctx, &role, `
		SELECT id, name, description, managed, content_type
		FROM role_definitions WHERE name = $1`, name)
	return role, err
}

func (s *Store) AssignableRoles(ctx context.Context, kind ResourceKind) ([]RoleDefinition, error) {
	roles := []RoleDefinition{}
	err := s.db.SelectContext(ctx, &roles, `
		SELECT id, name, description, managed, content_type
		FROM role_definitions
		WHERE content_type IS NULL OR content_type = $1
		ORDER BY managed DESC, name`, string(kind))
	return roles, err
}

type PrincipalKind string

const (
	UserPrincipal PrincipalKind = "user"
	TeamPrincipal PrincipalKind = "team"
)

type Assignment struct {
	RoleDefinitionID int64
	Resource         *Resource // nil denotes global scope
	PrincipalKind    PrincipalKind
	PrincipalID      int64
}

func (s *Store) Assign(ctx context.Context, assignment Assignment) error {
	return s.transaction(ctx, func(tx *sqlx.Tx) error {
		objectRoleID, err := ensureScopedRole(ctx, tx, assignment.RoleDefinitionID, assignment.Resource)
		if err != nil {
			return err
		}
		var statement string
		switch assignment.PrincipalKind {
		case UserPrincipal:
			statement = `INSERT INTO role_user_assignments (role_definition_id, user_id, object_role_id)
				VALUES ($1, $2, $3) ON CONFLICT (user_id, object_role_id) DO NOTHING`
		case TeamPrincipal:
			statement = `INSERT INTO role_team_assignments (role_definition_id, team_id, object_role_id)
				VALUES ($1, $2, $3) ON CONFLICT (team_id, object_role_id) DO NOTHING`
		default:
			return fmt.Errorf("unknown principal kind %q", assignment.PrincipalKind)
		}
		if _, err := tx.ExecContext(ctx, statement, assignment.RoleDefinitionID, assignment.PrincipalID, objectRoleID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `SELECT rebuild_object_role_evaluations($1)`, objectRoleID)
		return err
	})
}

func (s *Store) Revoke(ctx context.Context, assignment Assignment) error {
	if assignment.Resource == nil {
		return s.revokeGlobal(ctx, assignment)
	}
	table, principalColumn := assignmentTable(assignment.PrincipalKind)
	if table == "" {
		return fmt.Errorf("unknown principal kind %q", assignment.PrincipalKind)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+table+` a USING object_roles o
		WHERE a.object_role_id = o.id AND a.role_definition_id = $1
		AND a.`+principalColumn+` = $2 AND o.content_type = $3 AND o.object_id = $4`,
		assignment.RoleDefinitionID, assignment.PrincipalID, string(assignment.Resource.Kind), assignment.Resource.ID)
	return err
}

func (s *Store) revokeGlobal(ctx context.Context, assignment Assignment) error {
	table, principalColumn := assignmentTable(assignment.PrincipalKind)
	if table == "" {
		return fmt.Errorf("unknown principal kind %q", assignment.PrincipalKind)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+table+` a USING object_roles o
		WHERE a.object_role_id = o.id AND a.role_definition_id = $1
		AND a.`+principalColumn+` = $2 AND o.content_type IS NULL`, assignment.RoleDefinitionID, assignment.PrincipalID)
	return err
}

func assignmentTable(kind PrincipalKind) (string, string) {
	switch kind {
	case UserPrincipal:
		return "role_user_assignments", "user_id"
	case TeamPrincipal:
		return "role_team_assignments", "team_id"
	default:
		return "", ""
	}
}

func ensureScopedRole(ctx context.Context, tx *sqlx.Tx, definitionID int64, resource *Resource) (int64, error) {
	var id int64
	if resource == nil {
		err := tx.GetContext(ctx, &id, `SELECT id FROM object_roles WHERE role_definition_id = $1 AND content_type IS NULL`, definitionID)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		err = tx.GetContext(ctx, &id, `INSERT INTO object_roles (role_definition_id) VALUES ($1) RETURNING id`, definitionID)
		return id, err
	}
	err := tx.GetContext(ctx, &id, `SELECT id FROM object_roles
		WHERE role_definition_id = $1 AND content_type = $2 AND object_id = $3`, definitionID, string(resource.Kind), resource.ID)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = tx.GetContext(ctx, &id, `INSERT INTO object_roles (role_definition_id, content_type, object_id)
		VALUES ($1, $2, $3) RETURNING id`, definitionID, string(resource.Kind), resource.ID)
	return id, err
}

func (s *Store) transaction(ctx context.Context, operation func(*sqlx.Tx) error) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	if err := operation(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
