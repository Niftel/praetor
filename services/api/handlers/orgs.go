package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// OrgStore is the organizations-domain data access (incl. org-scoped sublists).
type OrgStore interface {
	ListAll(ctx context.Context, limit, offset int) ([]models.Organization, error)
	CountAll(ctx context.Context) (int64, error)
	ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Organization, error)
	Get(ctx context.Context, id int64) (models.Organization, error)
	Create(ctx context.Context, input models.Organization) (models.Organization, error)
	Update(ctx context.Context, input models.Organization) (models.Organization, error)
	Delete(ctx context.Context, id int64) (int64, error)
	UsersByRoleField(ctx context.Context, orgID int64, roleField string) ([]models.User, error)
	ListTeams(ctx context.Context, orgID int64) ([]models.Team, error)
	ListProjects(ctx context.Context, orgID int64) ([]models.Project, error)
	ListInventories(ctx context.Context, orgID int64) ([]models.Inventory, error)
	GalaxyCredentials(ctx context.Context, orgID int64) ([]store.OrgGalaxyCredential, error)
	AddGalaxyCredential(ctx context.Context, orgID, credentialID int64, position int) error
	RemoveGalaxyCredential(ctx context.Context, orgID, credentialID int64) error
}

// OrgsResource is the self-contained organizations domain (incl. org membership,
// sub-resource lists, and galaxy-credential links in galaxy_credentials.go),
// extracted from the former ContentHandler god-object (B6/#85).
type OrgsResource struct {
	DB *sqlx.DB
	*Authorizer
	store OrgStore
}

func NewOrgsResource(db *sqlx.DB) *OrgsResource {
	return &OrgsResource{DB: db, Authorizer: NewAuthorizer(db), store: store.NewOrgStore(db)}
}

// ListOrganizations GET /api/v1/organizations
// Returns organizations the user has read access to
func (h *OrgsResource) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	pg := render.ParsePagination(r)

	var orgs []models.Organization
	var total int64

	// Superusers and system auditors see all
	if userCtx.IsSuperuser {
		var err error
		if orgs, err = h.store.ListAll(r.Context(), pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total, _ = h.store.CountAll(r.Context())
	} else {
		// Filter by accessible organizations
		accessibleIDs, err := h.readableIDs(r, rbac.ContentTypeOrganization)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if orgs, err = h.store.ListByIDs(r.Context(), accessibleIDs, pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total = int64(len(accessibleIDs))
	}

	if orgs == nil {
		orgs = []models.Organization{}
	}

	render.JSON(w, r, &render.PaginatedResponse{
		Items:  orgs,
		Total:  total,
		Limit:  pg.Limit,
		Offset: pg.Offset,
	})
}

// CreateOrganization POST /api/v1/organizations
// Only superusers can create organizations (AWX behavior)
func (h *OrgsResource) CreateOrganization(w http.ResponseWriter, r *http.Request) {

	// Only superusers can create organizations
	if !currentUser(r).IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	var input models.Organization
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	created, err := h.store.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, created)
}

// GetOrganization GET /api/v1/organizations/{id}
func (h *OrgsResource) GetOrganization(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)

	// Check read permission
	if !h.authorize(w, r, rbac.ContentTypeOrganization, id, actRead) {
		return
	}

	org, err := h.store.Get(r.Context(), id)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	render.JSON(w, r, org)
}

// UpdateOrganization PUT /api/v1/organizations/{id}
func (h *OrgsResource) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)

	// Check admin permission
	if !h.authorize(w, r, rbac.ContentTypeOrganization, id, actAdmin) {
		return
	}

	var input models.Organization
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input.ID = id

	updated, err := h.store.Update(r.Context(), input)
	if errors.Is(err, sql.ErrNoRows) {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	} else if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, updated)
}

// DeleteOrganization DELETE /api/v1/organizations/{id}
// Only superusers can delete organizations
func (h *OrgsResource) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)

	if !currentUser(r).IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	count, err := h.store.Delete(r.Context(), id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if count == 0 {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	render.NoContent(w, r)
}

// ListOrganizationUsers GET /api/v1/organizations/{id}/users
// Returns users who are members of the organization (have member_role)
func (h *OrgsResource) ListOrganizationUsers(w http.ResponseWriter, r *http.Request) {
	id := getOrgIDFromPath(r)

	// Check read permission
	if !h.authorize(w, r, rbac.ContentTypeOrganization, id, actRead) {
		return
	}

	// Get all users who have the member_role for this org
	users, err := h.store.UsersByRoleField(r.Context(), id, "member_role")
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, users)
}

// AddOrganizationUser POST /api/v1/organizations/{id}/users
// Adds a user as member of the organization
func (h *OrgsResource) AddOrganizationUser(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromPath(r)

	// Check admin permission
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actAdmin) {
		return
	}

	type AddUserRequest struct {
		UserID int64 `json:"user_id"`
	}
	var req AddUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if err := h.setOrgRole(r.Context(), orgID, rbac.RoleFieldMember, req.UserID, true); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}

// RemoveOrganizationUser DELETE /api/v1/organizations/{id}/users/{userId}
func (h *OrgsResource) RemoveOrganizationUser(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromPath(r)
	userIDStr := chi.URLParam(r, "userId")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Check admin permission
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actAdmin) {
		return
	}

	if err := h.setOrgRole(r.Context(), orgID, rbac.RoleFieldMember, userID, false); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.NoContent(w, r)
}

// ListOrganizationAdmins GET /api/v1/organizations/{id}/admins
func (h *OrgsResource) ListOrganizationAdmins(w http.ResponseWriter, r *http.Request) {
	id := getOrgIDFromPath(r)

	// Check read permission
	if !h.authorize(w, r, rbac.ContentTypeOrganization, id, actRead) {
		return
	}

	users, err := h.store.UsersByRoleField(r.Context(), id, "admin_role")
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, users)
}

// AddOrganizationAdmin POST /api/v1/organizations/{id}/admins
func (h *OrgsResource) AddOrganizationAdmin(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromPath(r)

	// Only superusers or existing org admins can add admins
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actAdmin) {
		return
	}

	type AddUserRequest struct {
		UserID int64 `json:"user_id"`
	}
	var req AddUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if err := h.setOrgRole(r.Context(), orgID, rbac.RoleFieldAdmin, req.UserID, true); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}

// setOrgRole grants or revokes the managed RoleDefinition mirroring an org role_field
// (member/admin) for a user, scoped to the organization.
func (h *OrgsResource) setOrgRole(ctx context.Context, orgID int64, rf rbac.RoleField, userID int64, grant bool) error {
	name, ok := rbac.ManagedNameForLegacy(rbac.ContentTypeOrganization, rf)
	if !ok {
		return fmt.Errorf("no managed role definition for %s", rf)
	}
	def, err := h.caps.GetRoleDefinitionByName(ctx, name)
	if err != nil {
		return err
	}
	ct := string(rbac.ContentTypeOrganization)
	if grant {
		return h.caps.GiveUserPermission(ctx, def.ID, &ct, &orgID, userID)
	}
	return h.caps.RevokeUserPermission(ctx, def.ID, ct, orgID, userID)
}

// ListOrganizationTeams GET /api/v1/organizations/{id}/teams
func (h *OrgsResource) ListOrganizationTeams(w http.ResponseWriter, r *http.Request) {
	id := getOrgIDFromPath(r)

	if !h.authorize(w, r, rbac.ContentTypeOrganization, id, actRead) {
		return
	}

	teams, err := h.store.ListTeams(r.Context(), id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, teams)
}

// ListOrganizationRoles GET /api/v1/organizations/{id}/object_roles
// Returns all roles for this organization
func (h *OrgsResource) ListOrganizationRoles(w http.ResponseWriter, r *http.Request) {
	id := getOrgIDFromPath(r)

	if !h.authorize(w, r, rbac.ContentTypeOrganization, id, actRead) {
		return
	}

	roles, err := h.caps.AssignableRoles(r.Context(), string(rbac.ContentTypeOrganization))
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, roles)
}

// ListOrganizationProjects GET /api/v1/organizations/{id}/projects
func (h *OrgsResource) ListOrganizationProjects(w http.ResponseWriter, r *http.Request) {
	id := getOrgIDFromPath(r)

	if !h.authorize(w, r, rbac.ContentTypeOrganization, id, actRead) {
		return
	}

	projects, err := h.store.ListProjects(r.Context(), id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, projects)
}

// ListOrganizationInventories GET /api/v1/organizations/{id}/inventories
func (h *OrgsResource) ListOrganizationInventories(w http.ResponseWriter, r *http.Request) {
	id := getOrgIDFromPath(r)

	if !h.authorize(w, r, rbac.ContentTypeOrganization, id, actRead) {
		return
	}

	inventories, err := h.store.ListInventories(r.Context(), id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, inventories)
}

// Helper to extract org ID from path
func getOrgIDFromPath(r *http.Request) int64 {
	return render.GetIDParam(r) // Uses the {id} param
}

// Helper to get context (for consistency)
func getContext(r *http.Request) context.Context {
	return r.Context()
}
