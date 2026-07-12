package handlers

import (
	"context"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/plog"
	"github.com/praetordev/rbac"
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

// Authorizer is the shared object-level authorization helper (the Policy
// Enforcement Point), embedded by every resource handler. It translates HTTP
// requests into questions for the injected rbac.Authorizer (the decision point)
// and denials into responses; it holds no policy itself. The legacy is_superuser
// bypass no longer lives here — it is one decorator behind `authz`.
//
// Access is retained for the roles/org grant handlers that still write
// assignments through it; caps is retained for the creator-grant write path.
type Authorizer struct {
	Access *rbac.AccessChecker
	caps   *store.CapabilityStore
	authz  rbac.Authorizer
}

func NewAuthorizer(db *sqlx.DB) *Authorizer {
	caps := store.NewCapabilityStore(db)
	return &Authorizer{
		Access: rbac.NewAccessChecker(db),
		caps:   caps,
		authz:  rbac.WithLegacySystemFlags(caps),
	}
}

// currentUser pulls the authenticated user set by the auth middleware.
func currentUser(r *http.Request) middleware.UserContext {
	return r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
}

// subject builds the rbac.Subject for the current request. This is the one place
// the legacy system flags cross from the HTTP layer into the decision point;
// beyond it, code sees only capabilities.
func (a *Authorizer) subject(r *http.Request) rbac.Subject {
	uc := currentUser(r)
	return rbac.NewSubject(uc.UserID, uc.IsSuperuser, uc.IsSystemAuditor)
}

// authorize verifies the current user may perform action on (contentType, objectID)
// via the capability model. It writes the response and returns false when the
// request must stop — 403 if denied, 500 on error. The superuser short-circuit is
// applied inside the injected Authorizer (the legacy decorator).
//
//	if !h.authorize(w, r, ct, id, actAdmin) { return }
func (a *Authorizer) authorize(w http.ResponseWriter, r *http.Request, contentType rbac.ContentType, objectID int64, action permAction) bool {
	verb, ok := actionVerb[action]
	if !ok {
		render.ErrForbidden(nil).Render(w, r)
		return false
	}
	allowed, err := a.authz.Can(r.Context(), a.subject(r), verb, rbac.Obj(contentType, objectID))
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

// holdsGlobal reports whether the current user holds the global (system-scope)
// capability codename, via the injected Authorizer — so break-glass superusers
// (through the decorator) and any role that grants it globally (e.g. System
// Administrator) pass.
func (a *Authorizer) holdsGlobal(r *http.Request, codename string) (bool, error) {
	return a.authz.CanGlobal(r.Context(), a.subject(r), codename)
}

// requireGlobal stops the request with 403 unless the current user holds the
// global capability codename — 500 on a lookup error. It is the capability-model
// replacement for the old requireSuperuser flag gate, used by shared/system
// resources that have no per-org owner (execution packs, credential types, event
// sources).
//
//	if !rs.requireGlobal(w, r, rbac.CapManageExecutionPack) { return }
func (a *Authorizer) requireGlobal(w http.ResponseWriter, r *http.Request, codename string) bool {
	ok, err := a.holdsGlobal(r, codename)
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

// authorizeOrgRole gates an org-scoped create on the capability that permits creating the
// child type, checked on the organization. Org admins (who hold the add_* capability) and
// superusers pass. It writes the response and returns false when the request must stop.
func (a *Authorizer) authorizeOrgRole(w http.ResponseWriter, r *http.Request, orgID int64, roleField rbac.RoleField) bool {
	codename, ok := orgCreateCapability[roleField]
	if !ok {
		// Not a capability-modelled child type (e.g. notifications): require org manage.
		codename = rbac.Codename(rbac.ContentTypeOrganization, rbac.ActionManage)
	}
	// A cross-type check: the codename (e.g. add_project) is held ON the org
	// object, so it goes through CanCodename, not Can. Superuser short-circuits
	// inside the injected Authorizer.
	allowed, err := a.authz.CanCodename(r.Context(), a.subject(r), codename, rbac.Obj(rbac.ContentTypeOrganization, orgID))
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

// readableIDs returns the object IDs of contentType the current user may read.
// Break-glass superusers and any global view-granting system role (e.g. System
// Auditor) see everything; everyone else gets the objects they hold the view
// capability on. The tier unification lives in the injected Authorizer.
func (a *Authorizer) readableIDs(r *http.Request, contentType rbac.ContentType) ([]int64, error) {
	return a.authz.VisibleIDs(r.Context(), a.subject(r), rbac.ActionView, contentType)
}

// canViewAll reports whether the current user may view every object of
// contentType — a global view grant, held by break-glass superusers (via the
// decorator) and any system role that grants view globally (e.g. System
// Auditor, whose managed role grants view_* on every type). It is the fast-path
// condition for "list all" without per-object filtering, and the
// capability-model replacement for the old `IsSuperuser || IsSystemAuditor`
// list gate.
func (a *Authorizer) canViewAll(r *http.Request, contentType rbac.ContentType) (bool, error) {
	return a.holdsGlobal(r, rbac.Codename(contentType, rbac.ActionView))
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
