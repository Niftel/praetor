package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/api/render"
)

// ListUsers GET /api/v1/users
func (h *ContentHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)

	var users []models.User
	query := `SELECT id, username, first_name, last_name, email, is_superuser, is_active, created_at, modified_at FROM users ORDER BY id LIMIT $1 OFFSET $2`
	err := h.DB.Select(&users, query, pg.Limit, pg.Offset)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	var total int64
	_ = h.DB.Get(&total, "SELECT count(*) FROM users")

	if users == nil {
		users = []models.User{}
	}

	render.JSON(w, r, &render.PaginatedResponse{
		Items:  users,
		Total:  total,
		Limit:  pg.Limit,
		Offset: pg.Offset,
	})
}

// CreateUser POST /api/v1/users
func (h *ContentHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	// Simplified user creation (no password hashing implemented yet for skeleton)
	var input models.User
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Just assume password_hash is provided or empty for now
	query := `
		INSERT INTO users (username, password_hash, email, first_name, last_name, is_superuser) 
		VALUES (:username, 'placeholder_hash', :email, :first_name, :last_name, :is_superuser) 
		RETURNING id, username, email, first_name, last_name, is_superuser, is_active, created_at, modified_at`

	rows, err := h.DB.NamedQuery(query, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer rows.Close()

	if rows.Next() {
		var created models.User
		if err := rows.StructScan(&created); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.Created(w, r, created)
	} else {
		render.ErrInternal(nil).Render(w, r)
	}
}

// GetUser GET /api/v1/users/{id}
func (h *ContentHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	var user models.User
	err := h.DB.Get(&user, "SELECT id, username, first_name, last_name, email, is_superuser, is_active, created_at, modified_at FROM users WHERE id = $1", id)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	render.JSON(w, r, user)
}

// UpdateUser PUT /api/v1/users/{id}
func (h *ContentHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	var input models.User
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input.ID = id

	// Only updating standard fields for now. Password reset would be separate or handled carefully.
	query := `
		UPDATE users 
		SET email=:email, first_name=:first_name, last_name=:last_name, is_superuser=:is_superuser, is_active=:is_active, modified_at=NOW()
		WHERE id=:id
		RETURNING id, username, email, first_name, last_name, is_superuser, is_active, created_at, modified_at`

	rows, err := h.DB.NamedQuery(query, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer rows.Close()

	if rows.Next() {
		var updated models.User
		if err := rows.StructScan(&updated); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.JSON(w, r, updated)
	} else {
		render.Render(w, r, render.ErrNotFound(nil))
	}
}

// DeleteUser DELETE /api/v1/users/{id}
func (h *ContentHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	res, err := h.DB.Exec("DELETE FROM users WHERE id = $1", id)
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

// ListUserOrganizations GET /api/v1/users/{id}/organizations
// Returns organizations the user is a member of
func (h *ContentHandler) ListUserOrganizations(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)

	var orgs []models.Organization
	err := h.DB.Select(&orgs, `
		SELECT DISTINCT o.id, o.name, o.description, o.created_at, o.modified_at
		FROM organizations o
		JOIN roles r ON r.content_type = 'organization' AND r.object_id = o.id
		JOIN role_members rm ON rm.role_id = r.id
		WHERE rm.user_id = $1
		ORDER BY o.id
	`, userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if orgs == nil {
		orgs = []models.Organization{}
	}
	render.JSON(w, r, orgs)
}

// ListUserTeams GET /api/v1/users/{id}/teams
// Returns teams the user is a member of
func (h *ContentHandler) ListUserTeams(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)

	var teams []models.Team
	err := h.DB.Select(&teams, `
		SELECT DISTINCT t.id, t.organization_id, t.name, t.description, t.created_at, t.modified_at
		FROM teams t
		JOIN roles r ON r.content_type = 'team' AND r.object_id = t.id
		JOIN role_members rm ON rm.role_id = r.id
		WHERE rm.user_id = $1
		UNION
		SELECT DISTINCT t.id, t.organization_id, t.name, t.description, t.created_at, t.modified_at
		FROM teams t
		JOIN team_members tm ON tm.team_id = t.id
		WHERE tm.user_id = $1
		ORDER BY id
	`, userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if teams == nil {
		teams = []models.Team{}
	}
	render.JSON(w, r, teams)
}

// ListUserRoles GET /api/v1/users/{id}/roles
// Returns all roles the user has (directly or through teams)
func (h *ContentHandler) ListUserRoles(w http.ResponseWriter, r *http.Request) {
	userID := render.GetIDParam(r)

	roles, err := h.Access.GetUserRoles(r.Context(), userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, roles)
}
