package store

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
)

// userListCols / userReturnCols are the reduced user projections used by the
// user endpoints (no password_hash, no LDAP, no is_system_auditor), preserved
// verbatim from the original handler SQL.
const (
	userListCols   = `id, username, first_name, last_name, email, is_superuser, is_active, created_at, modified_at`
	userReturnCols = `id, username, email, first_name, last_name, is_superuser, is_active, created_at, modified_at`
)

// UserStore is the data-access layer for the users domain.
type UserStore struct {
	db *sqlx.DB
}

func NewUserStore(db *sqlx.DB) *UserStore { return &UserStore{db: db} }

// List returns a page of users.
func (s *UserStore) List(ctx context.Context, limit, offset int) ([]models.User, error) {
	users := []models.User{}
	err := s.db.SelectContext(ctx, &users, `SELECT `+userListCols+` FROM users ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
	return users, wrap("UserStore.List", err)
}

// Count returns the total number of users.
func (s *UserStore) Count(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.GetContext(ctx, &total, "SELECT count(*) FROM users")
	return total, wrap("UserStore.Count", err)
}

// Get returns a single user by id.
func (s *UserStore) Get(ctx context.Context, id int64) (models.User, error) {
	var user models.User
	err := s.db.GetContext(ctx, &user, `SELECT `+userListCols+` FROM users WHERE id = $1`, id)
	return user, wrap("UserStore.Get", err)
}

// ByUsernameWithHash loads a user including password_hash for login verification.
func (s *UserStore) ByUsernameWithHash(ctx context.Context, username string) (models.User, error) {
	var user models.User
	err := s.db.GetContext(ctx, &user,
		`SELECT id, username, password_hash, first_name, last_name, email, is_superuser, is_system_auditor, is_active, ldap_dn FROM users WHERE username = $1`,
		username)
	return user, wrap("UserStore.ByUsernameWithHash", err)
}

// Create inserts a user (PasswordHash precomputed by the caller) and returns it.
func (s *UserStore) Create(ctx context.Context, u models.User) (models.User, error) {
	query := `
		INSERT INTO users (username, password_hash, email, first_name, last_name, is_superuser)
		VALUES (:username, :password_hash, :email, :first_name, :last_name, :is_superuser)
		RETURNING ` + userReturnCols
	return s.namedReturning(query, u)
}

// Update applies an edit to a user. When setPassword is true the (precomputed)
// PasswordHash is written too. Returns the persisted row.
func (s *UserStore) Update(ctx context.Context, u models.User, setPassword bool) (models.User, error) {
	query := `
		UPDATE users
		SET email=:email, first_name=:first_name, last_name=:last_name, is_superuser=:is_superuser, is_active=:is_active, modified_at=NOW()`
	if setPassword {
		query += `, password_hash=:password_hash`
	}
	query += `
		WHERE id=:id
		RETURNING ` + userReturnCols
	return s.namedReturning(query, u)
}

func (s *UserStore) namedReturning(query string, arg models.User) (models.User, error) {
	var out models.User
	rows, err := s.db.NamedQuery(query, arg)
	if err != nil {
		return out, wrap("UserStore.namedReturning", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.StructScan(&out); err != nil {
			return out, wrap("UserStore.namedReturning", err)
		}
	}
	return out, nil
}

// Delete removes a user by id, returning rows affected (0 = did not exist).
func (s *UserStore) Delete(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM users WHERE id = $1", id)
	if err != nil {
		return 0, wrap("UserStore.Delete", err)
	}
	return res.RowsAffected()
}

// Organizations returns the organizations a user has any role on — capability model:
// an object_role scoped to the org that the user holds directly (role_user_assignments)
// or through a team (role_team_assignments + team_members).
func (s *UserStore) Organizations(ctx context.Context, userID int64) ([]models.Organization, error) {
	orgs := []models.Organization{}
	err := s.db.SelectContext(ctx, &orgs, `
		SELECT DISTINCT o.id, o.name, o.description, o.created_at, o.modified_at
		FROM organizations o
		JOIN object_roles orl ON orl.content_type = 'organization' AND orl.object_id = o.id
		WHERE EXISTS (SELECT 1 FROM role_user_assignments ua
		              WHERE ua.object_role_id = orl.id AND ua.user_id = $1)
		   OR EXISTS (SELECT 1 FROM role_team_assignments ta
		              JOIN team_members tm ON tm.team_id = ta.team_id
		              WHERE ta.object_role_id = orl.id AND tm.user_id = $1)
		ORDER BY o.id`, userID)
	return orgs, wrap("UserStore.Organizations", err)
}

// Teams returns the teams a user belongs to: direct membership (team_members) or a
// role held on the team via the capability model (object_role on the team, assigned
// to the user directly or through a team).
func (s *UserStore) Teams(ctx context.Context, userID int64) ([]models.Team, error) {
	teams := []models.Team{}
	err := s.db.SelectContext(ctx, &teams, `
		SELECT DISTINCT t.id, t.organization_id, t.name, t.description, t.created_at, t.modified_at
		FROM teams t
		JOIN object_roles orl ON orl.content_type = 'team' AND orl.object_id = t.id
		WHERE EXISTS (SELECT 1 FROM role_user_assignments ua
		              WHERE ua.object_role_id = orl.id AND ua.user_id = $1)
		   OR EXISTS (SELECT 1 FROM role_team_assignments ta
		              JOIN team_members tm ON tm.team_id = ta.team_id
		              WHERE ta.object_role_id = orl.id AND tm.user_id = $1)
		UNION
		SELECT DISTINCT t.id, t.organization_id, t.name, t.description, t.created_at, t.modified_at
		FROM teams t
		JOIN team_members tm ON tm.team_id = t.id
		WHERE tm.user_id = $1
		ORDER BY id`, userID)
	return teams, wrap("UserStore.Teams", err)
}
