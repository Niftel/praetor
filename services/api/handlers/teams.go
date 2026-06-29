package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/api/render"
)

// ... (existing code omitted for brevity in thought, but I must preserve it or just replace the parts)
// I better just replace the whole file content or use replace blocks carefully.
// I'll replace the top block to add imports, and the bottom block to fix RemoveTeamMember.

// Since I can't do multiple separate replacements in one go easily with replace_file_content unless I use multi_replace.
// I will use multi_replace_file_content.

// ListTeams GET /api/v1/teams
func (h *ContentHandler) ListTeams(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)
	var teams []models.Team
	query := `SELECT id, organization_id, name, description, created_at, modified_at FROM teams ORDER BY id LIMIT $1 OFFSET $2`
	err := h.DB.Select(&teams, query, pg.Limit, pg.Offset)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	var total int64
	_ = h.DB.Get(&total, "SELECT count(*) FROM teams")
	if teams == nil {
		teams = []models.Team{}
	}

	render.JSON(w, r, &render.PaginatedResponse{
		Items:  teams,
		Total:  total,
		Limit:  pg.Limit,
		Offset: pg.Offset,
	})
}

// CreateTeam POST /api/v1/teams
func (h *ContentHandler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	var input models.Team
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Default Org ID if not provided (simplified for now)
	if input.OrganizationID == 0 {
		input.OrganizationID = 1
	}

	query := `
		INSERT INTO teams (organization_id, name, description) 
		VALUES (:organization_id, :name, :description) 
		RETURNING id, organization_id, name, description, created_at, modified_at`

	rows, err := h.DB.NamedQuery(query, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer rows.Close()

	if rows.Next() {
		var created models.Team
		if err := rows.StructScan(&created); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.Created(w, r, created)
	} else {
		render.ErrInternal(nil).Render(w, r)
	}
}

// GetTeam GET /api/v1/teams/{id}
func (h *ContentHandler) GetTeam(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	var team models.Team
	err := h.DB.Get(&team, "SELECT id, organization_id, name, description, created_at, modified_at FROM teams WHERE id = $1", id)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	render.JSON(w, r, team)
}

// UpdateTeam PUT /api/v1/teams/{id}
func (h *ContentHandler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	var input models.Team
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input.ID = id

	query := `
		UPDATE teams 
		SET name=:name, description=:description, modified_at=NOW()
		WHERE id=:id
		RETURNING id, organization_id, name, description, created_at, modified_at`

	rows, err := h.DB.NamedQuery(query, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer rows.Close()

	if rows.Next() {
		var updated models.Team
		if err := rows.StructScan(&updated); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.JSON(w, r, updated)
	} else {
		render.Render(w, r, render.ErrNotFound(nil))
	}
}

// DeleteTeam DELETE /api/v1/teams/{id}
func (h *ContentHandler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	res, err := h.DB.Exec("DELETE FROM teams WHERE id = $1", id)
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

// AddTeamMember POST /api/v1/teams/{id}/members
func (h *ContentHandler) AddTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID := render.GetIDParam(r)

	type MemberRequest struct {
		UserID int64 `json:"user_id"`
	}
	var req MemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	query := `INSERT INTO team_members (team_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := h.DB.Exec(query, teamID, req.UserID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}

// ListTeamMembers GET /api/v1/teams/{id}/members
func (h *ContentHandler) ListTeamMembers(w http.ResponseWriter, r *http.Request) {
	teamID := render.GetIDParam(r)

	var members []models.User
	// Join users and team_members
	query := `
		SELECT u.id, u.username, u.first_name, u.last_name, u.email, u.is_superuser, u.is_active, u.created_at, u.modified_at
		FROM users u
		JOIN team_members tm ON u.id = tm.user_id
		WHERE tm.team_id = $1
	`
	err := h.DB.Select(&members, query, teamID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if members == nil {
		members = []models.User{}
	}
	render.JSON(w, r, members)
}

// RemoveTeamMember DELETE /api/v1/teams/{id}/members/{userID}
func (h *ContentHandler) RemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID := render.GetIDParam(r)

	userIDStr := chi.URLParam(r, "userID")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	query := `DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`
	_, err = h.DB.Exec(query, teamID, userID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}
