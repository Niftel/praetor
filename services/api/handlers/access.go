package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/accesscontrol"
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
	kind := accesscontrol.ResourceKind(ct)
	canRead, err := h.authz.CanCapability(r.Context(), h.subject(r), accesscontrol.Capability(kind, accesscontrol.View), accesscontrol.Object(kind, oid))
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

// Capabilities GET /api/v1/capabilities?content_type=&object_id=
// reports the authenticated user's effective permissions on one object. The
// result comes from the same policy decision point used by mutation handlers,
// including inherited team roles and break-glass access, so clients do not
// have to reconstruct RBAC from role names.
func (h *AccessResource) Capabilities(w http.ResponseWriter, r *http.Request) {
	kind := accesscontrol.ResourceKind(r.URL.Query().Get("content_type"))
	objectID, err := strconv.ParseInt(r.URL.Query().Get("object_id"), 10, 64)
	if err != nil || objectID <= 0 || !capabilityResourceKind(kind) {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	resource := accesscontrol.Object(kind, objectID)
	verbs := []accesscontrol.Verb{
		accesscontrol.View,
		accesscontrol.Manage,
		accesscontrol.Use,
		accesscontrol.Execute,
		accesscontrol.Update,
		accesscontrol.Approve,
	}
	out := make(map[string]bool, len(verbs)+2)
	for _, verb := range verbs {
		if !accesscontrol.IsCapability(kind, verb) {
			out[string(verb)] = false
			continue
		}
		allowed, decisionErr := h.authz.Can(r.Context(), h.subject(r), verb, resource)
		if decisionErr != nil {
			render.ErrInternal(decisionErr).Render(w, r)
			return
		}
		out[string(verb)] = allowed
	}

	if kind == accesscontrol.Organization {
		for name, childKind := range map[string]accesscontrol.ResourceKind{
			"add_inventory":         accesscontrol.Inventory,
			"add_workflow_template": accesscontrol.WorkflowTemplate,
		} {
			allowed, decisionErr := h.authz.CanCapability(r.Context(), h.subject(r), accesscontrol.Capability(childKind, accesscontrol.Add), resource)
			if decisionErr != nil {
				render.ErrInternal(decisionErr).Render(w, r)
				return
			}
			out[name] = allowed
		}
	}
	render.JSON(w, r, out)
}

func capabilityResourceKind(kind accesscontrol.ResourceKind) bool {
	switch kind {
	case accesscontrol.Organization, accesscontrol.Team, accesscontrol.Project,
		accesscontrol.Inventory, accesscontrol.Credential, accesscontrol.JobTemplate,
		accesscontrol.WorkflowTemplate:
		return true
	default:
		return false
	}
}

// AssignableRoles GET /api/v1/role-definitions?content_type= — the RoleDefinitions that
// can be granted on an object of the given type (populates the access picker).
func (h *AccessResource) AssignableRoles(w http.ResponseWriter, r *http.Request) {
	ct := r.URL.Query().Get("content_type")
	if ct == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	defs, err := h.caps.AssignableRoles(r.Context(), accesscontrol.ResourceKind(ct))
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
	kind := accesscontrol.ResourceKind(ct)
	ok, err := h.authz.CanCapability(r.Context(), h.subject(r), accesscontrol.Capability(kind, accesscontrol.Manage), accesscontrol.Object(kind, oid))
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
	if accesscontrol.ResourceKind(req.ContentType) == accesscontrol.Organization {
		return false, "" // granting an org role IS the membership
	}
	orgID, ok := h.Access.OrganizationFor(r.Context(), accesscontrol.Object(accesscontrol.ResourceKind(req.ContentType), req.ObjectID))
	if !ok {
		return false, ""
	}
	orgView := accesscontrol.Capability(accesscontrol.Organization, accesscontrol.View)
	switch {
	case req.UserID != nil:
		member, _ := h.authz.CanCapability(r.Context(), accesscontrol.Principal{UserID: *req.UserID}, orgView, accesscontrol.Object(accesscontrol.Organization, orgID))
		if !member {
			return true, "user must be a member of the resource's organization before being granted a role on it"
		}
	case req.TeamID != nil:
		if teamOrg, ok := h.Access.OrganizationFor(r.Context(), accesscontrol.Object(accesscontrol.Team, *req.TeamID)); !ok || teamOrg != orgID {
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
	resource := accesscontrol.Object(accesscontrol.ResourceKind(req.ContentType), req.ObjectID)
	switch {
	case req.UserID != nil:
		err = h.caps.Assign(r.Context(), accesscontrol.Assignment{RoleDefinitionID: req.RoleDefinitionID, Resource: &resource, PrincipalKind: accesscontrol.UserPrincipal, PrincipalID: *req.UserID})
	case req.TeamID != nil:
		err = h.caps.Assign(r.Context(), accesscontrol.Assignment{RoleDefinitionID: req.RoleDefinitionID, Resource: &resource, PrincipalKind: accesscontrol.TeamPrincipal, PrincipalID: *req.TeamID})
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
	resource := accesscontrol.Object(accesscontrol.ResourceKind(req.ContentType), req.ObjectID)
	switch {
	case req.UserID != nil:
		err = h.caps.Revoke(r.Context(), accesscontrol.Assignment{RoleDefinitionID: req.RoleDefinitionID, Resource: &resource, PrincipalKind: accesscontrol.UserPrincipal, PrincipalID: *req.UserID})
	case req.TeamID != nil:
		err = h.caps.Revoke(r.Context(), accesscontrol.Assignment{RoleDefinitionID: req.RoleDefinitionID, Resource: &resource, PrincipalKind: accesscontrol.TeamPrincipal, PrincipalID: *req.TeamID})
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
