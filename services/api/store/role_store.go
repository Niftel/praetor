package store

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
)

// RoleStore is the data-access layer for the roles domain reads the handlers do
// directly (role assignment mutations go through pkg/rbac's Access service).
type RoleStore struct {
	db *sqlx.DB
}

func NewRoleStore(db *sqlx.DB) *RoleStore { return &RoleStore{db: db} }

// ListAll returns all roles, ordered for display.
func (s *RoleStore) ListAll(ctx context.Context) ([]rbac.Role, error) {
	roles := []rbac.Role{}
	err := s.db.SelectContext(ctx, &roles,
		`SELECT `+RoleCols+` FROM roles ORDER BY content_type, object_id, role_field`)
	return roles, err
}

// ListSingletons returns the system singleton roles.
func (s *RoleStore) ListSingletons(ctx context.Context) ([]rbac.Role, error) {
	roles := []rbac.Role{}
	err := s.db.SelectContext(ctx, &roles,
		`SELECT `+RoleCols+` FROM roles WHERE singleton_name IS NOT NULL`)
	return roles, err
}

// GetByID returns a single role by id.
func (s *RoleStore) GetByID(ctx context.Context, id int64) (rbac.Role, error) {
	var role rbac.Role
	err := s.db.GetContext(ctx, &role, `SELECT `+RoleCols+` FROM roles WHERE id = $1`, id)
	return role, err
}

// UsersForRole returns the users directly assigned a role.
func (s *RoleStore) UsersForRole(ctx context.Context, roleID int64) ([]models.User, error) {
	users := []models.User{}
	err := s.db.SelectContext(ctx, &users, `
		SELECT `+orgUserCols+`
		FROM users u
		JOIN role_members rm ON u.id = rm.user_id
		WHERE rm.role_id = $1`, roleID)
	return users, err
}

// TeamsForRole returns the teams assigned a role.
func (s *RoleStore) TeamsForRole(ctx context.Context, roleID int64) ([]models.Team, error) {
	teams := []models.Team{}
	err := s.db.SelectContext(ctx, &teams, `
		SELECT t.id, t.organization_id, t.name, t.description, t.created_at, t.modified_at
		FROM teams t
		JOIN team_roles tr ON t.id = tr.team_id
		WHERE tr.role_id = $1`, roleID)
	return teams, err
}
