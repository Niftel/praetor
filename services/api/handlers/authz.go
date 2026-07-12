package handlers

import (
	"context"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/plog"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/render"
	"github.com/praetordev/praetor/services/api/store"
)

// logger is the api handlers component logger (handler installed by pkg/plog).
var logger = plog.New("api")

// permAction is the kind of access an endpoint requires on an object. Each maps to a DAB
// capability verb via actionVerb.
type permAction int

const (
	actRead permAction = iota
	actAdmin
	actUse
	actExecute
	actUpdate  // update_role: run an SCM update / inventory-source sync without admin
	actApprove // approval_role: approve or deny workflow approval nodes
)

// actionVerb maps a permAction to the capability verb it requires. actAdmin -> manage is
// the "administer" capability (see pkg/rbac).
var actionVerb = map[permAction]rbac.Action{
	actRead:    rbac.ActionView,
	actAdmin:   rbac.ActionManage,
	actUse:     rbac.ActionUse,
	actExecute: rbac.ActionExecute,
	actUpdate:  rbac.ActionUpdate,
	actApprove: rbac.ActionApprove,
}

// orgCreateCapability maps a delegated org admin role_field to the capability that gates
// creating that child type, checked on the organization object. Notifications are not a
// capability content type, so their create is gated on org manage instead (see
// authorizeOrgRole).
var orgCreateCapability = map[rbac.RoleField]string{
	rbac.RoleFieldProjectAdmin:     rbac.Codename(rbac.ContentTypeProject, rbac.ActionAdd),
	rbac.RoleFieldInventoryAdmin:   rbac.Codename(rbac.ContentTypeInventory, rbac.ActionAdd),
	rbac.RoleFieldCredentialAdmin:  rbac.Codename(rbac.ContentTypeCredential, rbac.ActionAdd),
	rbac.RoleFieldJobTemplateAdmin: rbac.Codename(rbac.ContentTypeJobTemplate, rbac.ActionAdd),
	rbac.RoleFieldWorkflowAdmin:    rbac.Codename(rbac.ContentTypeWorkflowTemplate, rbac.ActionAdd),
}

// Authorizer is the shared object-level authorization helper, embedded by every resource
// handler. Authorization runs entirely on the DAB capability model; Access is retained for
// the roles/org grant handlers that still write assignments through it.
type Authorizer struct {
	Access *rbac.AccessChecker
	caps   *store.CapabilityStore
}

func NewAuthorizer(db *sqlx.DB) *Authorizer {
	return &Authorizer{Access: rbac.NewAccessChecker(db), caps: store.NewCapabilityStore(db)}
}

// currentUser pulls the authenticated user set by the auth middleware.
func currentUser(r *http.Request) middleware.UserContext {
	return r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
}

// authorize verifies the current user may perform action on (contentType, objectID) via
// the capability model. It writes the response and returns false when the request must
// stop — 403 if denied, 500 on error. Superuser (all actions) and system auditor (reads)
// short-circuit.
//
//	if !h.authorize(w, r, ct, id, actAdmin) { return }
func (a *Authorizer) authorize(w http.ResponseWriter, r *http.Request, contentType rbac.ContentType, objectID int64, action permAction) bool {
	uc := currentUser(r)
	if uc.IsSuperuser {
		return true // break-glass superuser (DAB-standard); also holds the global System Administrator role
	}
	verb, ok := actionVerb[action]
	if !ok {
		render.ErrForbidden(nil).Render(w, r)
		return false
	}
	allowed, err := a.caps.HasCapability(r.Context(), uc.UserID, contentType, objectID, rbac.Codename(contentType, verb))
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return false
	}
	if !allowed {
		render.ErrForbidden(nil).Render(w, r)
		return false
	}
	return true
}

// requireSuperuser stops the request with 403 unless the caller is a superuser. For
// shared/system resources that have no per-org owner (e.g. execution packs).
func requireSuperuser(w http.ResponseWriter, r *http.Request) bool {
	if currentUser(r).IsSuperuser {
		return true
	}
	render.ErrForbidden(nil).Render(w, r)
	return false
}

// authorizeOrgRole gates an org-scoped create on the capability that permits creating the
// child type, checked on the organization. Org admins (who hold the add_* capability) and
// superusers pass. It writes the response and returns false when the request must stop.
func (a *Authorizer) authorizeOrgRole(w http.ResponseWriter, r *http.Request, orgID int64, roleField rbac.RoleField) bool {
	uc := currentUser(r)
	if uc.IsSuperuser {
		return true
	}
	codename, ok := orgCreateCapability[roleField]
	if !ok {
		// Not a capability-modelled child type (e.g. notifications): require org manage.
		codename = rbac.Codename(rbac.ContentTypeOrganization, rbac.ActionManage)
	}
	allowed, err := a.caps.HasCapability(r.Context(), uc.UserID, rbac.ContentTypeOrganization, orgID, codename)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return false
	}
	if !allowed {
		render.ErrForbidden(nil).Render(w, r)
		return false
	}
	return true
}

// readableIDs returns the object IDs of contentType the current user may read. Superusers
// and system auditors see everything; everyone else gets the objects they hold the view
// capability on.
func (a *Authorizer) readableIDs(r *http.Request, contentType rbac.ContentType) ([]int64, error) {
	uc := currentUser(r)
	view := rbac.Codename(contentType, rbac.ActionView)
	if uc.IsSuperuser {
		return a.caps.AllIDsOfType(r.Context(), contentType)
	}
	// A system role (e.g. System Auditor) that grants view globally sees everything.
	if global, err := a.caps.HasGlobalCapability(r.Context(), uc.UserID, view); err != nil {
		return nil, err
	} else if global {
		return a.caps.AllIDsOfType(r.Context(), contentType)
	}
	return a.caps.AccessibleIDs(r.Context(), uc.UserID, contentType, view)
}

// grantCreatorAdmin assigns the creating user the object's admin RoleDefinition so a
// non-superuser can manage what they create. Superusers already have implicit access, so
// they are skipped. Best-effort: a failure is logged, not surfaced.
func (a *Authorizer) grantCreatorAdmin(ctx context.Context, contentType rbac.ContentType, objectID int64, uc middleware.UserContext) {
	if uc.IsSuperuser {
		return
	}
	name, ok := rbac.ManagedNameForLegacy(contentType, rbac.RoleFieldAdmin)
	if !ok {
		logger.Error("authz: no admin role definition for content type", "content_type", contentType)
		return
	}
	def, err := a.caps.GetRoleDefinitionByName(ctx, name)
	if err != nil {
		logger.Error("authz: admin role definition not found", "name", name, "err", err)
		return
	}
	ct := string(contentType)
	if err := a.caps.GiveUserPermission(ctx, def.ID, &ct, &objectID, uc.UserID); err != nil {
		logger.Error("authz: grant creator admin failed", "user_id", uc.UserID, "content_type", contentType, "object_id", objectID, "err", err)
	}
}
