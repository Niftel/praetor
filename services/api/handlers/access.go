package handlers

import (
	"net/http"
	"strconv"

	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/praetor/services/api/render"
)

// access.go surfaces RBAC the AWX way: per-resource (who has which role on this
// object) and per-user (which roles a user holds), instead of a global role list.

type accessUser struct {
	ID        int64  `json:"id" db:"id"`
	Username  string `json:"username" db:"username"`
	FirstName string `json:"first_name" db:"first_name"`
	LastName  string `json:"last_name" db:"last_name"`
}

type accessTeam struct {
	ID   int64  `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

type accessRole struct {
	RoleID    int64        `json:"role_id"`
	RoleField string       `json:"role_field"`
	Users     []accessUser `json:"users"`
	Teams     []accessTeam `json:"teams"`
}

// ResourceAccess GET /api/v1/access?content_type=&object_id=
// Lists the object's roles, each with the users and teams that hold it. Add /
// remove is done through the existing /roles/{id}/users and /roles/{id}/teams
// endpoints (which already gate on admin of the parent object).
func (h *ContentHandler) ResourceAccess(w http.ResponseWriter, r *http.Request) {
	uc := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	ct := r.URL.Query().Get("content_type")
	oid, err := strconv.ParseInt(r.URL.Query().Get("object_id"), 10, 64)
	if ct == "" || err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	canRead, _ := h.Access.CanRead(r.Context(), uc.UserID, rbac.ContentType(ct), oid)
	if !canRead && !uc.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	roles, err := h.Access.GetObjectRoles(r.Context(), rbac.ContentType(ct), oid)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	out := make([]accessRole, 0, len(roles))
	for _, role := range roles {
		ar := accessRole{RoleID: role.ID, RoleField: role.RoleField, Users: []accessUser{}, Teams: []accessTeam{}}
		_ = h.DB.SelectContext(r.Context(), &ar.Users, `
			SELECT u.id, u.username, COALESCE(u.first_name,'') AS first_name, COALESCE(u.last_name,'') AS last_name
			FROM role_members rm JOIN users u ON u.id = rm.user_id
			WHERE rm.role_id = $1 ORDER BY u.username`, role.ID)
		_ = h.DB.SelectContext(r.Context(), &ar.Teams, `
			SELECT t.id, t.name FROM team_roles tr JOIN teams t ON t.id = tr.team_id
			WHERE tr.role_id = $1 ORDER BY t.name`, role.ID)
		out = append(out, ar)
	}
	render.JSON(w, r, out)
}

type userAccessRole struct {
	RoleID       int64   `json:"role_id" db:"role_id"`
	RoleField    string  `json:"role_field" db:"role_field"`
	ContentType  string  `json:"content_type" db:"content_type"`
	ObjectID     *int64  `json:"object_id" db:"object_id"`
	Singleton    *string `json:"singleton_name" db:"singleton_name"`
	ResourceName *string `json:"resource_name" db:"resource_name"`
}

// UserAccess GET /api/v1/users/{id}/access — the roles a user holds directly,
// resolved to a resource name (team membership shows as the team's member role).
func (h *ContentHandler) UserAccess(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)
	rows := []userAccessRole{}
	err := h.DB.SelectContext(r.Context(), &rows, `
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
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}
