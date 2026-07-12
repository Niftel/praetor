package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
)

// teamListCols is the projection the /teams endpoints return — deliberately a
// reduced Team view (no LDAP bookkeeping), preserved byte-for-byte from the
// original handler SQL. (Note: TeamCols, used by the org sublist, is the fuller
// set — an existing divergence kept intentionally to avoid changing responses.)
const teamListCols = `id, organization_id, name, description, created_at, modified_at`

// teamMemberUserCols is the reduced user projection returned by team-member
// listings (note: no is_system_auditor — preserved from the original SQL).
const teamMemberUserCols = `u.id, u.username, u.first_name, u.last_name, u.email, u.is_superuser, u.is_active, u.created_at, u.modified_at`

// TeamStore is the data-access layer for the teams domain (incl. team_members).
type TeamStore struct {
	db *sqlx.DB
}

func NewTeamStore(db *sqlx.DB) *TeamStore { return &TeamStore{db: db} }

// ListAll returns a page of all teams.
func (s *TeamStore) ListAll(ctx context.Context, limit, offset int) ([]models.Team, error) {
	teams := []models.Team{}
	err := s.db.SelectContext(ctx, &teams, `SELECT `+teamListCols+` FROM teams ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
	return teams, wrap("TeamStore.ListAll", err)
}

// CountAll returns the total number of teams.
func (s *TeamStore) CountAll(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.GetContext(ctx, &total, "SELECT count(*) FROM teams")
	return total, wrap("TeamStore.CountAll", err)
}

// ListByIDs returns a page of the teams whose id is in ids.
func (s *TeamStore) ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Team, error) {
	teams := []models.Team{}
	if len(ids) == 0 {
		return teams, nil
	}
	q, args, err := sqlx.In(`SELECT `+teamListCols+` FROM teams WHERE id IN (?) ORDER BY id LIMIT ? OFFSET ?`, ids, limit, offset)
	if err != nil {
		return nil, wrap("TeamStore.ListByIDs", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &teams, q, args...)
	return teams, wrap("TeamStore.ListByIDs", err)
}

// Get returns a single team by id.
func (s *TeamStore) Get(ctx context.Context, id int64) (models.Team, error) {
	var team models.Team
	err := s.db.GetContext(ctx, &team, "SELECT "+teamListCols+" FROM teams WHERE id = $1", id)
	return team, wrap("TeamStore.Get", err)
}

// Create inserts a team and returns the persisted row.
func (s *TeamStore) Create(ctx context.Context, input models.Team) (models.Team, error) {
	query := `
		INSERT INTO teams (organization_id, name, description)
		VALUES (:organization_id, :name, :description)
		RETURNING ` + teamListCols
	return s.namedReturning(query, input)
}

// Update applies an edit to a team (input.ID must be set) and returns it.
func (s *TeamStore) Update(ctx context.Context, input models.Team) (models.Team, error) {
	query := `
		UPDATE teams
		SET name=:name, description=:description, modified_at=NOW()
		WHERE id=:id
		RETURNING ` + teamListCols
	return s.namedReturning(query, input)
}

func (s *TeamStore) namedReturning(query string, arg models.Team) (models.Team, error) {
	var out models.Team
	rows, err := s.db.NamedQuery(query, arg)
	if err != nil {
		return out, wrap("TeamStore.namedReturning", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.StructScan(&out); err != nil {
			return out, wrap("TeamStore.namedReturning", err)
		}
		return out, nil
	}
	return out, sql.ErrNoRows
}

// Delete removes a team by id, returning rows affected (0 = did not exist).
func (s *TeamStore) Delete(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM teams WHERE id = $1", id)
	if err != nil {
		return 0, wrap("TeamStore.Delete", err)
	}
	return res.RowsAffected()
}

// AddMember adds a user to a team (idempotent).
func (s *TeamStore) AddMember(ctx context.Context, teamID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO team_members (team_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, teamID, userID)
	return wrap("TeamStore.AddMember", err)
}

// RemoveMember removes a user from a team.
func (s *TeamStore) RemoveMember(ctx context.Context, teamID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`, teamID, userID)
	return wrap("TeamStore.RemoveMember", err)
}

// Members returns the users belonging to a team.
func (s *TeamStore) Members(ctx context.Context, teamID int64) ([]models.User, error) {
	members := []models.User{}
	err := s.db.SelectContext(ctx, &members, `
		SELECT `+teamMemberUserCols+`
		FROM users u
		JOIN team_members tm ON u.id = tm.user_id
		WHERE tm.team_id = $1`, teamID)
	return members, wrap("TeamStore.Members", err)
}
