package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// ListRoles GET /api/v1/roles
// Lists all roles user can see (system roles + roles on accessible objects)
func (h *ContentHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)

	var roles []rbac.Role
	var err error

	if userCtx.IsSuperuser {
		// Superusers see all roles
		err = h.DB.Select(&roles, `SELECT `+store.RoleCols+`
			FROM roles ORDER BY content_type, object_id, role_field`)
	} else {
		// Regular users see system roles + roles on objects they can access
		roles, err = h.Access.GetUserRoles(r.Context(), userCtx.UserID)
	}

	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Also include system singleton roles
	var singletons []rbac.Role
	h.DB.Select(&singletons, `SELECT `+store.RoleCols+`
		FROM roles WHERE singleton_name IS NOT NULL`)

	// Merge (avoiding duplicates)
	roleSet := make(map[int64]rbac.Role)
	for _, r := range singletons {
		roleSet[r.ID] = r
	}
	for _, r := range roles {
		roleSet[r.ID] = r
	}

	result := make([]rbac.Role, 0, len(roleSet))
	for _, r := range roleSet {
		result = append(result, r)
	}

	render.JSON(w, r, result)
}

// GetRole GET /api/v1/roles/{id}
func (h *ContentHandler) GetRole(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	roleIDStr := chi.URLParam(r, "id")
	roleID, err := strconv.ParseInt(roleIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var role rbac.Role
	err = h.DB.Get(&role, `
		SELECT id, role_field, singleton_name, content_type, object_id, name, description, created_at, modified_at
		FROM roles WHERE id = $1
	`, roleID)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}

	// For object roles, check if user can view the object
	if role.ContentType != nil && role.ObjectID != nil {
		canRead, err := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentType(*role.ContentType), *role.ObjectID)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if !canRead && !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	}

	render.JSON(w, r, role)
}

// ListRoleUsers GET /api/v1/roles/{id}/users
func (h *ContentHandler) ListRoleUsers(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	roleIDStr := chi.URLParam(r, "id")
	roleID, err := strconv.ParseInt(roleIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Get role to check permissions
	var role rbac.Role
	err = h.DB.Get(&role, `SELECT `+store.RoleCols+` FROM roles WHERE id = $1`, roleID)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}

	// Check permission on parent object
	if role.ContentType != nil && role.ObjectID != nil {
		canRead, _ := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentType(*role.ContentType), *role.ObjectID)
		if !canRead && !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	}

	var users []models.User
	err = h.DB.Select(&users, `
		SELECT u.id, u.username, u.first_name, u.last_name, u.email, 
		       u.is_superuser, u.is_system_auditor, u.is_active, u.created_at, u.modified_at
		FROM users u
		JOIN role_members rm ON u.id = rm.user_id
		WHERE rm.role_id = $1
	`, roleID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if users == nil {
		users = []models.User{}
	}
	render.JSON(w, r, users)
}

// AddRoleUser POST /api/v1/roles/{id}/users
func (h *ContentHandler) AddRoleUser(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	roleIDStr := chi.URLParam(r, "id")
	roleID, err := strconv.ParseInt(roleIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var role rbac.Role
	err = h.DB.Get(&role, `SELECT `+store.RoleCols+` FROM roles WHERE id = $1`, roleID)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}

	// Check admin permission on parent object
	if role.ContentType != nil && role.ObjectID != nil {
		canAdmin, _ := h.Access.CanAdmin(r.Context(), userCtx.UserID, rbac.ContentType(*role.ContentType), *role.ObjectID)
		if !canAdmin && !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	} else if role.SingletonName != nil {
		// System roles - only superuser can assign
		if !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	}

	type AddUserRequest struct {
		UserID int64 `json:"user_id"`
	}
	var req AddUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	err = h.Access.AddUserToRole(r.Context(), roleID, req.UserID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.NoContent(w, r)
}

// RemoveRoleUser DELETE /api/v1/roles/{id}/users/{userId}
func (h *ContentHandler) RemoveRoleUser(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	roleIDStr := chi.URLParam(r, "id")
	roleID, err := strconv.ParseInt(roleIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	userIDStr := chi.URLParam(r, "userId")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var role rbac.Role
	err = h.DB.Get(&role, `SELECT `+store.RoleCols+` FROM roles WHERE id = $1`, roleID)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}

	// Check admin permission
	if role.ContentType != nil && role.ObjectID != nil {
		canAdmin, _ := h.Access.CanAdmin(r.Context(), userCtx.UserID, rbac.ContentType(*role.ContentType), *role.ObjectID)
		if !canAdmin && !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	} else if role.SingletonName != nil {
		if !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	}

	err = h.Access.RemoveUserFromRole(r.Context(), roleID, userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.NoContent(w, r)
}

// ListRoleTeams GET /api/v1/roles/{id}/teams
func (h *ContentHandler) ListRoleTeams(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	roleIDStr := chi.URLParam(r, "id")
	roleID, err := strconv.ParseInt(roleIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var role rbac.Role
	err = h.DB.Get(&role, `SELECT `+store.RoleCols+` FROM roles WHERE id = $1`, roleID)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}

	if role.ContentType != nil && role.ObjectID != nil {
		canRead, _ := h.Access.CanRead(r.Context(), userCtx.UserID, rbac.ContentType(*role.ContentType), *role.ObjectID)
		if !canRead && !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	}

	var teams []models.Team
	err = h.DB.Select(&teams, `
		SELECT t.id, t.organization_id, t.name, t.description, t.created_at, t.modified_at
		FROM teams t
		JOIN team_roles tr ON t.id = tr.team_id
		WHERE tr.role_id = $1
	`, roleID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if teams == nil {
		teams = []models.Team{}
	}
	render.JSON(w, r, teams)
}

// AddRoleTeam POST /api/v1/roles/{id}/teams
func (h *ContentHandler) AddRoleTeam(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	roleIDStr := chi.URLParam(r, "id")
	roleID, err := strconv.ParseInt(roleIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var role rbac.Role
	err = h.DB.Get(&role, `SELECT `+store.RoleCols+` FROM roles WHERE id = $1`, roleID)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}

	// Check admin permission
	if role.ContentType != nil && role.ObjectID != nil {
		canAdmin, _ := h.Access.CanAdmin(r.Context(), userCtx.UserID, rbac.ContentType(*role.ContentType), *role.ObjectID)
		if !canAdmin && !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	} else if role.SingletonName != nil {
		// Can't assign teams to system roles
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	type AddTeamRequest struct {
		TeamID int64 `json:"team_id"`
	}
	var req AddTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	err = h.Access.AddTeamToRole(r.Context(), roleID, req.TeamID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.NoContent(w, r)
}

// RemoveRoleTeam DELETE /api/v1/roles/{id}/teams/{teamId}
func (h *ContentHandler) RemoveRoleTeam(w http.ResponseWriter, r *http.Request) {
	userCtx := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	roleIDStr := chi.URLParam(r, "id")
	roleID, err := strconv.ParseInt(roleIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	teamIDStr := chi.URLParam(r, "teamId")
	teamID, err := strconv.ParseInt(teamIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var role rbac.Role
	err = h.DB.Get(&role, `SELECT `+store.RoleCols+` FROM roles WHERE id = $1`, roleID)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}

	if role.ContentType != nil && role.ObjectID != nil {
		canAdmin, _ := h.Access.CanAdmin(r.Context(), userCtx.UserID, rbac.ContentType(*role.ContentType), *role.ObjectID)
		if !canAdmin && !userCtx.IsSuperuser {
			render.ErrForbidden(nil).Render(w, r)
			return
		}
	}

	err = h.Access.RemoveTeamFromRole(r.Context(), roleID, teamID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.NoContent(w, r)
}
