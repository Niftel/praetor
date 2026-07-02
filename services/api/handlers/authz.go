package handlers

import (
	"context"
	"log"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/praetor/services/api/render"
)

// permAction is the kind of access an endpoint requires on an object. Each maps
// to an AWX object role field via the AccessChecker.
type permAction int

const (
	actRead permAction = iota
	actAdmin
	actUse
	actExecute
)

// Authorizer is the shared object-level authorization helper. It is embedded by
// every resource handler so the same enforcement primitives (authorize,
// readableIDs, grantCreatorAdmin) are available everywhere, not just on
// ContentHandler.
type Authorizer struct {
	Access *rbac.AccessChecker
}

func NewAuthorizer(db *sqlx.DB) *Authorizer {
	return &Authorizer{Access: rbac.NewAccessChecker(db)}
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
// Superuser (all actions) and system auditor (reads) are handled inside the
// checker, so they are not special-cased here.
func (a *Authorizer) authorize(w http.ResponseWriter, r *http.Request, contentType rbac.ContentType, objectID int64, action permAction) bool {
	uc := currentUser(r)

	var allowed bool
	var err error
	switch action {
	case actRead:
		allowed, err = a.Access.CanRead(r.Context(), uc.UserID, contentType, objectID)
	case actAdmin:
		allowed, err = a.Access.CanAdmin(r.Context(), uc.UserID, contentType, objectID)
	case actUse:
		allowed, err = a.Access.CanUse(r.Context(), uc.UserID, contentType, objectID)
	case actExecute:
		allowed, err = a.Access.CanExecute(r.Context(), uc.UserID, contentType, objectID)
	}
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
		log.Printf("authz: could not find admin_role for %s/%d to grant creator: %v", contentType, objectID, err)
		return
	}
	if err := a.Access.AddUserToRole(ctx, role.ID, uc.UserID); err != nil {
		log.Printf("authz: could not grant creator %d admin on %s/%d: %v", uc.UserID, contentType, objectID, err)
	}
}
