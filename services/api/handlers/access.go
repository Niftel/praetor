package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
)

// AccessStore is the access/audit read access (per-resource access, user access,
// activity stream).
type AccessStore interface {
	ObjectAccess(ctx context.Context, contentType string, objectID int64) ([]store.ObjectRoleAccess, error)
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

func NewAccessResource(db *sqlx.DB, authz *Authorizer) *AccessResource {
	return &AccessResource{DB: db, Authorizer: authz, store: store.NewAccessStore(db)}
}

// access.go surfaces RBAC the AWX way: per-resource (who holds which RoleDefinition on
// this object) and per-user (which roles a user holds), instead of a global role list.

// ResourceAccess GET /api/v1/access?content_type=&object_id=
// Lists the object's RoleDefinition assignments, each with the users and teams holding it.
func (h *AccessResource) ResourceAccess(w http.ResponseWriter, r *http.Request) {
	ct := r.URL.Query().Get("content_type")
	oid, err := strconv.ParseInt(r.URL.Query().Get("object_id"), 10, 64)
	if ct == "" || err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	// You may view an object's access list if you can view the object — break-glass
	// superusers (via the decorator) and global-view system roles (auditor) pass.
	canRead, err := h.authz.CanCodename(r.Context(), h.subject(r), rbac.Codename(rbac.ContentType(ct), rbac.ActionView), rbac.Obj(rbac.ContentType(ct), oid))
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canRead {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	out, err := h.store.ObjectAccess(r.Context(), ct, oid)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

// UserAccess GET /api/v1/users/{id}/access — the capability roles a user holds directly,
// resolved to a resource name.
func (h *AccessResource) UserAccess(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)
	rows, err := h.store.UserAccessRoles(r.Context(), userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}

// AssignableRoles GET /api/v1/role-definitions?content_type= — the RoleDefinitions that
// can be granted on an object of the given type (populates the access picker).
func (h *AccessResource) AssignableRoles(w http.ResponseWriter, r *http.Request) {
	ct := r.URL.Query().Get("content_type")
	if ct == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	defs, err := h.caps.AssignableRoles(r.Context(), ct)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, defs)
}

type accessGrantRequest struct {
	ContentType      string `json:"content_type"`
	ObjectID         int64  `json:"object_id"`
	RoleDefinitionID int64  `json:"role_definition_id"`
	UserID           *int64 `json:"user_id,omitempty"`
	TeamID           *int64 `json:"team_id,omitempty"`
}

// gateManage lets the request through only if the caller may administer the object.
func (h *AccessResource) gateManage(w http.ResponseWriter, r *http.Request, ct string, oid int64) bool {
	// Administering an object's grants requires manage on it — superusers pass via
	// the decorator inside the injected Authorizer.
	ok, err := h.authz.CanCodename(r.Context(), h.subject(r), rbac.Codename(rbac.ContentType(ct), rbac.ActionManage), rbac.Obj(rbac.ContentType(ct), oid))
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return false
	}
	if !ok {
		render.ErrForbidden(nil).Render(w, r)
		return false
	}
	return true
}

// orgFenceViolated reports whether granting on a non-org resource would breach the org
// fence — the target user/team must belong to the resource's organization. Returns the
// message to surface when it does.
func (h *AccessResource) orgFenceViolated(r *http.Request, req accessGrantRequest) (bool, string) {
	if rbac.ContentType(req.ContentType) == rbac.ContentTypeOrganization {
		return false, "" // granting an org role IS the membership
	}
	orgID, ok := h.Access.OrgForContent(r.Context(), rbac.ContentType(req.ContentType), req.ObjectID)
	if !ok {
		return false, ""
	}
	orgView := rbac.Codename(rbac.ContentTypeOrganization, rbac.ActionView)
	switch {
	case req.UserID != nil:
		member, _ := h.caps.HasCapability(r.Context(), *req.UserID, rbac.ContentTypeOrganization, orgID, orgView)
		if !member {
			return true, "user must be a member of the resource's organization before being granted a role on it"
		}
	case req.TeamID != nil:
		if teamOrg, ok := h.Access.OrgForContent(r.Context(), rbac.ContentTypeTeam, *req.TeamID); !ok || teamOrg != orgID {
			return true, "team must belong to the resource's organization"
		}
	}
	return false, ""
}

// GrantAccess POST /api/v1/access — assign a RoleDefinition on an object to a user or team.
func (h *AccessResource) GrantAccess(w http.ResponseWriter, r *http.Request) {
	var req accessGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if req.ContentType == "" || req.ObjectID == 0 || req.RoleDefinitionID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !h.gateManage(w, r, req.ContentType, req.ObjectID) {
		return
	}
	// Org fence (AWX rule): a user/team may only be granted a role on a non-org resource
	// if they belong to that resource's organization (org membership confers view_org).
	if fenced, msg := h.orgFenceViolated(r, req); fenced {
		render.ErrForbidden(fmt.Errorf("%s", msg)).Render(w, r)
		return
	}
	var err error
	switch {
	case req.UserID != nil:
		err = h.caps.GiveUserPermission(r.Context(), req.RoleDefinitionID, &req.ContentType, &req.ObjectID, *req.UserID)
	case req.TeamID != nil:
		err = h.caps.GiveTeamPermission(r.Context(), req.RoleDefinitionID, &req.ContentType, &req.ObjectID, *req.TeamID)
	default:
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}

// RevokeAccess DELETE /api/v1/access — remove a user/team assignment of a RoleDefinition.
func (h *AccessResource) RevokeAccess(w http.ResponseWriter, r *http.Request) {
	var req accessGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if req.ContentType == "" || req.ObjectID == 0 || req.RoleDefinitionID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !h.gateManage(w, r, req.ContentType, req.ObjectID) {
		return
	}
	var err error
	switch {
	case req.UserID != nil:
		err = h.caps.RevokeUserPermission(r.Context(), req.RoleDefinitionID, req.ContentType, req.ObjectID, *req.UserID)
	case req.TeamID != nil:
		err = h.caps.RevokeTeamPermission(r.Context(), req.RoleDefinitionID, req.ContentType, req.ObjectID, *req.TeamID)
	default:
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}
