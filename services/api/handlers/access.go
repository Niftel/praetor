package handlers

import (
	"net/http"
	"strconv"

	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// access.go surfaces RBAC the AWX way: per-resource (who has which role on this
// object) and per-user (which roles a user holds), instead of a global role list.

type accessRole struct {
	RoleID    int64              `json:"role_id"`
	RoleField string             `json:"role_field"`
	Users     []store.AccessUser `json:"users"`
	Teams     []store.AccessTeam `json:"teams"`
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
		ar := accessRole{RoleID: role.ID, RoleField: role.RoleField, Users: []store.AccessUser{}, Teams: []store.AccessTeam{}}
		if u, err := h.access.RoleUsers(r.Context(), role.ID); err == nil {
			ar.Users = u
		}
		if t, err := h.access.RoleTeams(r.Context(), role.ID); err == nil {
			ar.Teams = t
		}
		out = append(out, ar)
	}
	render.JSON(w, r, out)
}

// UserAccess GET /api/v1/users/{id}/access — the roles a user holds directly,
// resolved to a resource name (team membership shows as the team's member role).
func (h *ContentHandler) UserAccess(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)
	rows, err := h.access.UserAccessRoles(r.Context(), userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}
