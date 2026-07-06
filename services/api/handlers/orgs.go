package handlers

import (
	"context"
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

type ContentHandler struct {
	DB *sqlx.DB
	*Authorizer
}

func NewContentHandler(db *sqlx.DB) *ContentHandler {
	return &ContentHandler{
		DB:         db,
		Authorizer: NewAuthorizer(db),
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
		query := `SELECT ` + store.OrganizationCols + ` FROM organizations ORDER BY id LIMIT $1 OFFSET $2`
		err := h.DB.Select(&orgs, query, pg.Limit, pg.Offset)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		_ = h.DB.Get(&total, "SELECT count(*) FROM organizations")
	} else {
		// Filter by accessible organizations
		accessibleIDs, err := h.Access.FilterAccessibleIDs(r.Context(), userCtx.UserID, rbac.ContentTypeOrganization, rbac.RoleFieldRead)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}

		if len(accessibleIDs) == 0 {
			orgs = []models.Organization{}
			total = 0
		} else {
			query, args, _ := sqlx.In(`SELECT `+store.OrganizationCols+` FROM organizations WHERE id IN (?) ORDER BY id LIMIT ? OFFSET ?`, accessibleIDs, pg.Limit, pg.Offset)
			query = h.DB.Rebind(query)
			err = h.DB.Select(&orgs, query, args...)
			if err != nil {
				render.ErrInternal(err).Render(w, r)
				return
			}
			total = int64(len(accessibleIDs))
		}
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

	query := `
		INSERT INTO organizations (name, description) 
		VALUES (:name, :description) 
		RETURNING ` + store.OrganizationCols

	rows, err := h.DB.NamedQuery(query, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer rows.Close()

	if rows.Next() {
		var created models.Organization
		if err := rows.StructScan(&created); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.Created(w, r, created)
	} else {
		render.ErrInternal(nil).Render(w, r)
	}
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

	var org models.Organization
	err = h.DB.Get(&org, "SELECT "+store.OrganizationCols+" FROM organizations WHERE id = $1", id)
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

	query := `
		UPDATE organizations 
		SET name=:name, description=:description, modified_at=NOW()
		WHERE id=:id
		RETURNING ` + store.OrganizationCols

	rows, err := h.DB.NamedQuery(query, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer rows.Close()

	if rows.Next() {
		var updated models.Organization
		if err := rows.StructScan(&updated); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.JSON(w, r, updated)
	} else {
		render.Render(w, r, render.ErrNotFound(nil))
	}
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

	res, err := h.DB.Exec("DELETE FROM organizations WHERE id = $1", id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	count, _ := res.RowsAffected()
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
	var users []models.User
	err = h.DB.Select(&users, `
		SELECT DISTINCT u.id, u.username, u.first_name, u.last_name, u.email, 
		       u.is_superuser, u.is_system_auditor, u.is_active, u.created_at, u.modified_at
		FROM users u
		JOIN role_members rm ON u.id = rm.user_id
		JOIN roles r ON rm.role_id = r.id
		WHERE r.content_type = 'organization' AND r.object_id = $1 AND r.role_field = 'member_role'
	`, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if users == nil {
		users = []models.User{}
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

	var users []models.User
	err = h.DB.Select(&users, `
		SELECT DISTINCT u.id, u.username, u.first_name, u.last_name, u.email, 
		       u.is_superuser, u.is_system_auditor, u.is_active, u.created_at, u.modified_at
		FROM users u
		JOIN role_members rm ON u.id = rm.user_id
		JOIN roles r ON rm.role_id = r.id
		WHERE r.content_type = 'organization' AND r.object_id = $1 AND r.role_field = 'admin_role'
	`, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if users == nil {
		users = []models.User{}
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

	var teams []models.Team
	err = h.DB.Select(&teams, `SELECT `+store.TeamCols+` FROM teams WHERE organization_id = $1 ORDER BY id`, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if teams == nil {
		teams = []models.Team{}
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

	var projects []models.Project
	err = h.DB.Select(&projects, `SELECT `+store.ProjectCols+` FROM projects WHERE organization_id = $1 ORDER BY id`, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if projects == nil {
		projects = []models.Project{}
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

	var inventories []models.Inventory
	err = h.DB.Select(&inventories, `SELECT `+store.InventoryCols+` FROM inventories WHERE organization_id = $1 ORDER BY id`, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if inventories == nil {
		inventories = []models.Inventory{}
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
