package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
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
}

// ProjectStore is the projects-domain data access the content handler depends on.
type ProjectStore interface {
	ListAll(ctx context.Context, limit, offset int) ([]models.Project, error)
	CountAll(ctx context.Context) (int64, error)
	ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Project, error)
	Get(ctx context.Context, id int64) (models.Project, error)
	Create(ctx context.Context, input models.Project) (models.Project, error)
	TouchModified(ctx context.Context, id int64) error
}

type ContentHandler struct {
	DB *sqlx.DB
	*Authorizer
	orgs     OrgStore
	projects ProjectStore
}

func NewContentHandler(db *sqlx.DB) *ContentHandler {
	return &ContentHandler{
		DB:         db,
		Authorizer: NewAuthorizer(db),
		orgs:       store.NewOrgStore(db),
		projects:   store.NewProjectStore(db),
	}
}

// ListOrganizations GET /api/v1/organizations
// Returns organizations the user has read access to
func (h *ContentHandler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	pg := render.ParsePagination(r)

	var orgs []models.Organization
	var total int64

	// Superusers and system auditors see all
	if userCtx.IsSuperuser {
		var err error
		if orgs, err = h.orgs.ListAll(r.Context(), pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total, _ = h.orgs.CountAll(r.Context())
	} else {
		// Filter by accessible organizations
		accessibleIDs, err := h.Access.FilterAccessibleIDs(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, rbac.RoleFieldRead)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if orgs, err = h.orgs.ListByIDs(r.Context(), accessibleIDs, pg.Limit, pg.Offset); err != nil {
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
func (h *ContentHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)

	// Only superusers can create organizations
	if !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	var input models.Organization
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	created, err := h.orgs.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, created)
}

// GetOrganization GET /api/v1/organizations/{id}
func (h *ContentHandler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := render.GetIDParam(r)

	// Check read permission
	canRead, err := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canRead && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	org, err := h.orgs.Get(r.Context(), id)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	render.JSON(w, r, org)
}

// UpdateOrganization PUT /api/v1/organizations/{id}
func (h *ContentHandler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := render.GetIDParam(r)

	// Check admin permission
	canAdmin, err := h.Access.CanAdmin(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canAdmin && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	var input models.Organization
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input.ID = id

	updated, err := h.orgs.Update(r.Context(), input)
	if err == sql.ErrNoRows {
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
func (h *ContentHandler) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := render.GetIDParam(r)

	if !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	count, err := h.orgs.Delete(r.Context(), id)
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
func (h *ContentHandler) ListOrganizationUsers(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := getOrgIDFromPath(r)

	// Check read permission
	canRead, err := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canRead && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	// Get all users who have the member_role for this org
	users, err := h.orgs.UsersByRoleField(r.Context(), id, "member_role")
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, users)
}

// AddOrganizationUser POST /api/v1/organizations/{id}/users
// Adds a user as member of the organization
func (h *ContentHandler) AddOrganizationUser(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	orgID := getOrgIDFromPath(r)

	// Check admin permission
	canAdmin, err := h.Access.CanAdmin(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, orgID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canAdmin && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
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

	// Get the member_role for this org
	role, err := h.Access.GetObjectRole(r.Context(), rbac.ContentTypeOrganization, orgID, rbac.RoleFieldMember)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Add user to role
	err = h.Access.AddUserToRole(r.Context(), role.ID, req.UserID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.NoContent(w, r)
}

// RemoveOrganizationUser DELETE /api/v1/organizations/{id}/users/{userId}
func (h *ContentHandler) RemoveOrganizationUser(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	orgID := getOrgIDFromPath(r)
	userIDStr := chi.URLParam(r, "userId")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Check admin permission
	canAdmin, err := h.Access.CanAdmin(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, orgID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canAdmin && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	// Get the member_role for this org
	role, err := h.Access.GetObjectRole(r.Context(), rbac.ContentTypeOrganization, orgID, rbac.RoleFieldMember)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Remove user from role
	err = h.Access.RemoveUserFromRole(r.Context(), role.ID, userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.NoContent(w, r)
}

// ListOrganizationAdmins GET /api/v1/organizations/{id}/admins
func (h *ContentHandler) ListOrganizationAdmins(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := getOrgIDFromPath(r)

	// Check read permission
	canRead, err := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canRead && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	users, err := h.orgs.UsersByRoleField(r.Context(), id, "admin_role")
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, users)
}

// AddOrganizationAdmin POST /api/v1/organizations/{id}/admins
func (h *ContentHandler) AddOrganizationAdmin(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	orgID := getOrgIDFromPath(r)

	// Only superusers or existing org admins can add admins
	canAdmin, err := h.Access.CanAdmin(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, orgID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canAdmin && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
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

	role, err := h.Access.GetObjectRole(r.Context(), rbac.ContentTypeOrganization, orgID, rbac.RoleFieldAdmin)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	err = h.Access.AddUserToRole(r.Context(), role.ID, req.UserID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.NoContent(w, r)
}

// ListOrganizationTeams GET /api/v1/organizations/{id}/teams
func (h *ContentHandler) ListOrganizationTeams(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := getOrgIDFromPath(r)

	canRead, err := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canRead && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	teams, err := h.orgs.ListTeams(r.Context(), id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, teams)
}

// ListOrganizationRoles GET /api/v1/organizations/{id}/object_roles
// Returns all roles for this organization
func (h *ContentHandler) ListOrganizationRoles(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := getOrgIDFromPath(r)

	canRead, err := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canRead && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	roles, err := h.Access.GetObjectRoles(r.Context(), rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, roles)
}

// ListOrganizationProjects GET /api/v1/organizations/{id}/projects
func (h *ContentHandler) ListOrganizationProjects(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := getOrgIDFromPath(r)

	canRead, err := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canRead && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	projects, err := h.orgs.ListProjects(r.Context(), id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, projects)
}

// ListOrganizationInventories GET /api/v1/organizations/{id}/inventories
func (h *ContentHandler) ListOrganizationInventories(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	id := getOrgIDFromPath(r)

	canRead, err := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !canRead && !userCtx.IsSuperuser {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	inventories, err := h.orgs.ListInventories(r.Context(), id)
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
