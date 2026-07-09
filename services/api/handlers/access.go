package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// AccessStore is the access/audit read access (per-resource access, user access,
// activity stream).
type AccessStore interface {
	RoleUsers(ctx context.Context, roleID int64) ([]store.AccessUser, error)
	RoleTeams(ctx context.Context, roleID int64) ([]store.AccessTeam, error)
	UserAccessRoles(ctx context.Context, userID int64) ([]store.UserAccessRole, error)
	ActivityStream(ctx context.Context, resourceType, action string, limit int) ([]store.ActivityEntry, error)
}

// AccessResource is the self-contained access/audit domain — per-resource access,
// user access, and the activity stream (activity.go) — extracted from
// ContentHandler (B6/#85).
type AccessResource struct {
	DB *sqlx.DB
	*Authorizer
	store AccessStore
}

func NewAccessResource(db *sqlx.DB) *AccessResource {
	return &AccessResource{DB: db, Authorizer: NewAuthorizer(db), store: store.NewAccessStore(db)}
}

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
func (h *AccessResource) ResourceAccess(w http.ResponseWriter, r *http.Request) {
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
		if u, err := h.store.RoleUsers(r.Context(), role.ID); err == nil {
			ar.Users = u
		}
		if t, err := h.store.RoleTeams(r.Context(), role.ID); err == nil {
			ar.Teams = t
		}
		out = append(out, ar)
	}
	render.JSON(w, r, out)
}

// UserAccess GET /api/v1/users/{id}/access — the roles a user holds directly,
// resolved to a resource name (team membership shows as the team's member role).
func (h *AccessResource) UserAccess(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)
	rows, err := h.store.UserAccessRoles(r.Context(), userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}
