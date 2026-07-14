package authorization

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	legacy "github.com/praetordev/praetor/pkg/rbac"
	engine "github.com/praetordev/rbac/v4"
)

type PostgresResolver struct {
	db     *sqlx.DB
	tables map[legacy.ContentType]string
}

func NewPostgres(db *sqlx.DB, tables map[legacy.ContentType]string) (*Authorizer, error) {
	return New(&PostgresResolver{db: db, tables: tables})
}

const actorHolds = `(
	EXISTS (SELECT 1 FROM role_user_assignments ua WHERE ua.object_role_id = orl.id AND ua.user_id = $1)
	OR EXISTS (SELECT 1 FROM role_team_assignments ta
	           JOIN team_members tm ON tm.team_id = ta.team_id
	           WHERE ta.object_role_id = orl.id AND tm.user_id = $1)
)`

type grantRow struct {
	Capability string `db:"capability"`
	Scope      string `db:"scope"`
}

func toGrants(rows []grantRow) []engine.Grant {
	grants := make([]engine.Grant, 0, len(rows))
	for _, row := range rows {
		grants = append(grants, engine.Grant{Capability: row.Capability, Scope: row.Scope, Effect: engine.Allow})
	}
	return grants
}

func (r *PostgresResolver) GlobalGrants(ctx context.Context, userID int64) ([]engine.Grant, error) {
	rows := []grantRow{}
	err := r.db.SelectContext(ctx, &rows, `
		SELECT DISTINCT p.codename AS capability, '' AS scope
		FROM object_roles orl
		JOIN role_definition_permissions rdp ON rdp.role_definition_id = orl.role_definition_id
		JOIN dab_permissions p ON p.id = rdp.permission_id
		WHERE orl.content_type IS NULL AND `+actorHolds, userID)
	return toGrants(rows), err
}

func (r *PostgresResolver) ObjectGrants(ctx context.Context, userID int64, contentType legacy.ContentType, objectID int64) ([]engine.Grant, error) {
	rows := []grantRow{}
	err := r.db.SelectContext(ctx, &rows, `
		SELECT DISTINCT p.codename AS capability, '' AS scope
		FROM object_roles orl
		JOIN role_definition_permissions rdp ON rdp.role_definition_id = orl.role_definition_id
		JOIN dab_permissions p ON p.id = rdp.permission_id
		WHERE orl.content_type IS NULL AND `+actorHolds+`
		UNION
		SELECT DISTINCT e.codename AS capability, e.content_type || ':' || e.object_id AS scope
		FROM role_evaluations e
		JOIN object_roles orl ON orl.id = e.object_role_id
		WHERE e.content_type = $2 AND e.object_id = $3 AND `+actorHolds,
		userID, string(contentType), objectID)
	return toGrants(rows), err
}

func (r *PostgresResolver) ScopedGrants(ctx context.Context, userID int64, contentType legacy.ContentType) ([]engine.Grant, error) {
	rows := []grantRow{}
	err := r.db.SelectContext(ctx, &rows, `
		SELECT DISTINCT e.codename AS capability, e.content_type || ':' || e.object_id AS scope
		FROM role_evaluations e
		JOIN object_roles orl ON orl.id = e.object_role_id
		WHERE e.content_type = $2 AND `+actorHolds+`
		ORDER BY scope, capability`, userID, string(contentType))
	return toGrants(rows), err
}

func (r *PostgresResolver) AllIDsOfType(ctx context.Context, contentType legacy.ContentType) ([]int64, error) {
	table, ok := r.tables[contentType]
	if !ok {
		return nil, fmt.Errorf("no table registered for content type %q", contentType)
	}
	ids := []int64{}
	err := r.db.SelectContext(ctx, &ids, `SELECT id FROM `+table+` ORDER BY id`)
	return ids, err
}
