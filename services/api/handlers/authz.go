package handlers

import (
	"context"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/env"
	"github.com/praetordev/praetor/pkg/plog"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// logger is the api handlers component logger (handler installed by pkg/plog).
var logger = plog.New("api")

// permAction is the kind of access an endpoint requires on an object. Each maps
// to an AWX object role field via the AccessChecker.
type permAction int

const (
	actRead permAction = iota
	actAdmin
	actUse
	actExecute
	actUpdate  // update_role: run an SCM update / inventory-source sync without admin
	actApprove // approval_role: approve or deny workflow approval nodes
)

// actionVerb maps a permAction to the DAB capability verb the capability check uses
// (Gitea #97). actAdmin -> manage is the "administer" capability (see pkg/rbac).
var actionVerb = map[permAction]rbac.Action{
	actRead:    rbac.ActionView,
	actAdmin:   rbac.ActionManage,
	actUse:     rbac.ActionUse,
	actExecute: rbac.ActionExecute,
	actUpdate:  rbac.ActionUpdate,
	actApprove: rbac.ActionApprove,
}

func (a permAction) String() string {
	if v, ok := actionVerb[a]; ok {
		return string(v)
	}
	return "unknown"
}

// rbacMode selects how authorize() decides (Gitea #97, epic #93). The legacy AWX-style
// hierarchy is on a deprecation track; the capability model is the target.
//
//	dual (default) — allow if legacy OR capability grants; log every divergence. Never
//	                 regresses access (legacy still governs) while surfacing where the
//	                 capability model disagrees, so the legacy path can be removed once
//	                 the divergence logs go quiet.
//	capability     — capability model only (post-cutover).
//	legacy         — legacy hierarchy only (emergency rollback).
type rbacMode string

const (
	modeDual       rbacMode = "dual"
	modeCapability rbacMode = "capability"
	modeLegacy     rbacMode = "legacy"
)

func resolveRBACMode() rbacMode {
	switch rbacMode(env.String("PRAETOR_RBAC_MODE", string(modeDual))) {
	case modeCapability:
		return modeCapability
	case modeLegacy:
		return modeLegacy
	default:
		return modeDual
	}
}

// Authorizer is the shared object-level authorization helper. It is embedded by
// every resource handler so the same enforcement primitives (authorize,
// readableIDs, grantCreatorAdmin) are available everywhere, not just on
// ContentHandler.
type Authorizer struct {
	Access *rbac.AccessChecker
	caps   *store.CapabilityStore
	mode   rbacMode
}

func NewAuthorizer(db *sqlx.DB) *Authorizer {
	return &Authorizer{
		Access: rbac.NewAccessChecker(db),
		caps:   store.NewCapabilityStore(db),
		mode:   resolveRBACMode(),
	}
}

// currentUser pulls the authenticated user set by the auth middleware.
func currentUser(r *http.Request) middleware.UserContext {
	return r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
}

// authorize verifies the current user may perform action on (contentType,
// objectID). It writes the appropriate response and returns false when the
// request must stop — 403 if denied, 500 on a checker error — so callers do:
//
//	if !h.authorize(w, r, ct, id, actAdmin) { return }
//
// Superuser (all actions) and system auditor (reads) short-circuit here, so both the
// legacy and capability paths agree on them regardless of backfill state.
func (a *Authorizer) authorize(w http.ResponseWriter, r *http.Request, contentType rbac.ContentType, objectID int64, action permAction) bool {
	uc := currentUser(r)
	if uc.IsSuperuser {
		return true
	}
	if action == actRead && uc.IsSystemAuditor {
		return true
	}

	allowed, err := a.decide(r.Context(), uc.UserID, contentType, objectID, action)
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

// decide runs the legacy and/or capability check per the configured mode. In dual mode it
// allows if either grants (no access regression) and logs any divergence so the legacy
// path can be retired once the logs are quiet.
func (a *Authorizer) decide(ctx context.Context, userID int64, contentType rbac.ContentType, objectID int64, action permAction) (bool, error) {
	var legacyOK, capOK bool
	var err error
	if a.mode != modeCapability {
		if legacyOK, err = a.legacyCheck(ctx, userID, contentType, objectID, action); err != nil {
			return false, err
		}
	}
	if a.mode != modeLegacy {
		if capOK, err = a.capabilityCheck(ctx, userID, contentType, objectID, action); err != nil {
			return false, err
		}
	}
	switch a.mode {
	case modeLegacy:
		return legacyOK, nil
	case modeCapability:
		return capOK, nil
	default: // dual
		if legacyOK != capOK {
			logger.Warn("rbac divergence (dual mode)",
				"user_id", userID, "content_type", contentType, "object_id", objectID,
				"action", action.String(), "legacy", legacyOK, "capability", capOK)
		}
		return legacyOK || capOK, nil
	}
}

// legacyCheck is the AWX-style hierarchy check (the pre-#97 behaviour).
func (a *Authorizer) legacyCheck(ctx context.Context, userID int64, contentType rbac.ContentType, objectID int64, action permAction) (bool, error) {
	switch action {
	case actRead:
		return a.Access.CanRead(ctx, userID, contentType, objectID)
	case actAdmin:
		return a.Access.CanAdmin(ctx, userID, contentType, objectID)
	case actUse:
		return a.Access.CanUse(ctx, userID, contentType, objectID)
	case actExecute:
		return a.Access.CanExecute(ctx, userID, contentType, objectID)
	case actUpdate:
		return a.Access.HasObjectRole(ctx, userID, contentType, objectID, rbac.RoleFieldUpdate)
	case actApprove:
		return a.Access.HasObjectRole(ctx, userID, contentType, objectID, rbac.RoleFieldApproval)
	}
	return false, nil
}

// capabilityCheck is the DAB capability-model check (the target behaviour).
func (a *Authorizer) capabilityCheck(ctx context.Context, userID int64, contentType rbac.ContentType, objectID int64, action permAction) (bool, error) {
	verb, ok := actionVerb[action]
	if !ok {
		return false, nil
	}
	return a.caps.HasCapability(ctx, userID, contentType, objectID, rbac.Codename(contentType, verb))
}

// requireSuperuser stops the request with 403 unless the caller is a superuser.
// For shared/system resources that have no per-org owner (e.g. execution packs,
// which are runtime infrastructure selected by templates across every org).
func requireSuperuser(w http.ResponseWriter, r *http.Request) bool {
	if currentUser(r).IsSuperuser {
		return true
	}
	render.ErrForbidden(nil).Render(w, r)
	return false
}

// authorizeOrgRole gates an org-scoped action on a delegated organization role
// (e.g. project_admin_role for creating a project). Org admins, system admins,
// and superusers pass automatically through the role hierarchy, so this is
// strictly wider than the plain org-admin check it replaces on create paths.
// It writes the response and returns false when the request must stop.
func (a *Authorizer) authorizeOrgRole(w http.ResponseWriter, r *http.Request, orgID int64, roleField rbac.RoleField) bool {
	uc := currentUser(r)
	allowed, err := a.Access.HasObjectRole(r.Context(), uc.UserID, rbac.ContentTypeOrganization, orgID, roleField)
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
// FilterAccessibleIDs already returns everything for superusers and system
// auditors, so list handlers can use this uniformly.
func (a *Authorizer) readableIDs(r *http.Request, contentType rbac.ContentType) ([]int64, error) {
	uc := currentUser(r)
	return a.Access.FilterAccessibleIDs(r.Context(), uc.UserID, contentType, rbac.RoleFieldRead)
}

// grantCreatorAdmin makes the creating user an admin of a freshly-created
// object so a non-superuser can manage what they create (AWX assigns the
// creator the object's admin_role). Superusers already have implicit access, so
// they are skipped. Best-effort: a failure is logged, not surfaced, since the
// object was already created.
func (a *Authorizer) grantCreatorAdmin(ctx context.Context, contentType rbac.ContentType, objectID int64, uc middleware.UserContext) {
	if uc.IsSuperuser {
		return
	}
	role, err := a.Access.GetObjectRole(ctx, contentType, objectID, rbac.RoleFieldAdmin)
	if err != nil {
		logger.Error("authz: admin_role not found to grant creator", "content_type", contentType, "object_id", objectID, "err", err)
		return
	}
	if err := a.Access.AddUserToRole(ctx, role.ID, uc.UserID); err != nil {
		logger.Error("authz: grant creator admin failed", "user_id", uc.UserID, "content_type", contentType, "object_id", objectID, "err", err)
	}
}
