package store

import (
	"context"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
)

// AccessUser is the compact user view in a resource's access listing.
type AccessUser struct {
	ID        int64  `json:"id" db:"id"`
	Username  string `json:"username" db:"username"`
	FirstName string `json:"first_name" db:"first_name"`
	LastName  string `json:"last_name" db:"last_name"`
}

// AccessTeam is the compact team view in a resource's access listing.
type AccessTeam struct {
	ID   int64  `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// UserAccessRole is a role a user holds, resolved to its resource name.
type UserAccessRole struct {
	RoleID       int64   `json:"role_id" db:"role_id"`
	RoleField    string  `json:"role_field" db:"role_field"`
	ContentType  string  `json:"content_type" db:"content_type"`
	ObjectID     *int64  `json:"object_id" db:"object_id"`
	Singleton    *string `json:"singleton_name" db:"singleton_name"`
	ResourceName *string `json:"resource_name" db:"resource_name"`
}

// ActivityEntry is a row of the activity_stream audit log.
type ActivityEntry struct {
	ID           int64     `json:"id" db:"id"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UserID       *int64    `json:"user_id" db:"user_id"`
	Username     string    `json:"username" db:"username"`
	Action       string    `json:"action" db:"action"`
	ResourceType string    `json:"resource_type" db:"resource_type"`
	ResourceID   *int64    `json:"resource_id" db:"resource_id"`
	Method       string    `json:"method" db:"method"`
	Path         string    `json:"path" db:"path"`
	StatusCode   int       `json:"status_code" db:"status_code"`
}

// AccessStore is the data-access layer for the access/audit read endpoints.
type AccessStore struct {
	db *sqlx.DB
}

func NewAccessStore(db *sqlx.DB) *AccessStore { return &AccessStore{db: db} }

// RoleUsers returns the users directly holding a role (compact view).
func (s *AccessStore) RoleUsers(ctx context.Context, roleID int64) ([]AccessUser, error) {
	users := []AccessUser{}
	err := s.db.SelectContext(ctx, &users, `
		SELECT u.id, u.username, COALESCE(u.first_name,'') AS first_name, COALESCE(u.last_name,'') AS last_name
		FROM role_members rm JOIN users u ON u.id = rm.user_id
		WHERE rm.role_id = $1 ORDER BY u.username`, roleID)
	return users, wrap("AccessStore.RoleUsers", err)
}

// RoleTeams returns the teams holding a role (compact view).
func (s *AccessStore) RoleTeams(ctx context.Context, roleID int64) ([]AccessTeam, error) {
	teams := []AccessTeam{}
	err := s.db.SelectContext(ctx, &teams, `
		SELECT t.id, t.name FROM team_roles tr JOIN teams t ON t.id = tr.team_id
		WHERE tr.role_id = $1 ORDER BY t.name`, roleID)
	return teams, wrap("AccessStore.RoleTeams", err)
}

// UserAccessRoles returns the roles a user holds directly, resolved to a name.
func (s *AccessStore) UserAccessRoles(ctx context.Context, userID int64) ([]UserAccessRole, error) {
	rows := []UserAccessRole{}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT r.id AS role_id, r.role_field, COALESCE(r.content_type, '') AS content_type,
		       r.object_id, r.singleton_name,
		       CASE r.content_type
		         WHEN 'organization' THEN (SELECT name FROM organizations WHERE id = r.object_id)
		         WHEN 'team'         THEN (SELECT name FROM teams         WHERE id = r.object_id)
		         WHEN 'project'      THEN (SELECT name FROM projects      WHERE id = r.object_id)
		         WHEN 'inventory'    THEN (SELECT name FROM inventories   WHERE id = r.object_id)
		         WHEN 'job_template' THEN (SELECT name FROM job_templates WHERE id = r.object_id)
		         WHEN 'credential'   THEN (SELECT name FROM credentials   WHERE id = r.object_id)
		       END AS resource_name
		FROM role_members rm
		JOIN roles r ON r.id = rm.role_id
		WHERE rm.user_id = $1
		ORDER BY r.content_type NULLS FIRST, resource_name, r.role_field`, userID)
	return rows, wrap("AccessStore.UserAccessRoles", err)
}

// ActivityStream returns audit entries, optionally filtered by resource type and
// action, newest first, capped at limit.
func (s *AccessStore) ActivityStream(ctx context.Context, resourceType, action string, limit int) ([]ActivityEntry, error) {
	query := `SELECT id, created_at, user_id, username, action, resource_type, resource_id, method, path, status_code
	          FROM activity_stream WHERE 1=1`
	args := []interface{}{}
	if resourceType != "" {
		args = append(args, resourceType)
		query += " AND resource_type = $" + strconv.Itoa(len(args))
	}
	if action != "" {
		args = append(args, action)
		query += " AND action = $" + strconv.Itoa(len(args))
	}
	args = append(args, limit)
	query += " ORDER BY created_at DESC LIMIT $" + strconv.Itoa(len(args))

	entries := []ActivityEntry{}
	err := s.db.SelectContext(ctx, &entries, query, args...)
	return entries, wrap("AccessStore.ActivityStream", err)
}
