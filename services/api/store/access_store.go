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

// ObjectRoleAccess is one RoleDefinition granted on an object, with its holders.
type ObjectRoleAccess struct {
	ObjectRoleID     int64        `json:"object_role_id" db:"object_role_id"`
	RoleDefinitionID int64        `json:"role_definition_id" db:"role_definition_id"`
	Role             string       `json:"role" db:"role"`
	Managed          bool         `json:"managed" db:"managed"`
	Users            []AccessUser `json:"users"`
	Teams            []AccessTeam `json:"teams"`
}

// UserAccessRole is a capability role a user holds, resolved to its resource name. A NULL
// content_type is a global/system role.
type UserAccessRole struct {
	Role         string  `json:"role" db:"role"`
	ContentType  *string `json:"content_type" db:"content_type"`
	ObjectID     *int64  `json:"object_id" db:"object_id"`
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

// resourceNameCase resolves an object's display name from its content_type/object_id.
const resourceNameCase = `
	CASE orl.content_type
	  WHEN 'organization'      THEN (SELECT name FROM organizations      WHERE id = orl.object_id)
	  WHEN 'team'              THEN (SELECT name FROM teams              WHERE id = orl.object_id)
	  WHEN 'project'           THEN (SELECT name FROM projects           WHERE id = orl.object_id)
	  WHEN 'inventory'         THEN (SELECT name FROM inventories        WHERE id = orl.object_id)
	  WHEN 'job_template'      THEN (SELECT name FROM job_templates      WHERE id = orl.object_id)
	  WHEN 'workflow_template' THEN (SELECT name FROM workflow_templates WHERE id = orl.object_id)
	  WHEN 'credential'        THEN (SELECT name FROM credentials        WHERE id = orl.object_id)
	END`

// ObjectAccess returns the RoleDefinitions granted on an object, each with its holders.
func (s *AccessStore) ObjectAccess(ctx context.Context, contentType string, objectID int64) ([]ObjectRoleAccess, error) {
	roles := []ObjectRoleAccess{}
	if err := s.db.SelectContext(ctx, &roles, `
		SELECT orl.id AS object_role_id, d.id AS role_definition_id, d.name AS role, d.managed
		FROM object_roles orl JOIN role_definitions d ON d.id = orl.role_definition_id
		WHERE orl.content_type = $1 AND orl.object_id = $2
		ORDER BY d.name`, contentType, objectID); err != nil {
		return nil, wrap("AccessStore.ObjectAccess", err)
	}
	for i := range roles {
		users := []AccessUser{}
		if err := s.db.SelectContext(ctx, &users, `
			SELECT u.id, u.username, COALESCE(u.first_name,'') AS first_name, COALESCE(u.last_name,'') AS last_name
			FROM role_user_assignments ua JOIN users u ON u.id = ua.user_id
			WHERE ua.object_role_id = $1 ORDER BY u.username`, roles[i].ObjectRoleID); err == nil {
			roles[i].Users = users
		}
		teams := []AccessTeam{}
		if err := s.db.SelectContext(ctx, &teams, `
			SELECT t.id, t.name FROM role_team_assignments ta JOIN teams t ON t.id = ta.team_id
			WHERE ta.object_role_id = $1 ORDER BY t.name`, roles[i].ObjectRoleID); err == nil {
			roles[i].Teams = teams
		}
	}
	return roles, nil
}

// UserAccessRoles returns the capability roles a user holds (direct assignments),
// resolved to a resource name; a NULL content_type is a global/system role.
func (s *AccessStore) UserAccessRoles(ctx context.Context, userID int64) ([]UserAccessRole, error) {
	rows := []UserAccessRole{}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT d.name AS role, orl.content_type, orl.object_id, `+resourceNameCase+` AS resource_name
		FROM role_user_assignments ua
		JOIN object_roles orl ON orl.id = ua.object_role_id
		JOIN role_definitions d ON d.id = ua.role_definition_id
		WHERE ua.user_id = $1
		ORDER BY orl.content_type NULLS FIRST, resource_name, d.name`, userID)
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
