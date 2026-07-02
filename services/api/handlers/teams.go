package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

// ListTeams GET /api/v1/teams
func (h *ContentHandler) ListTeams(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)
	uc := currentUser(r)

	var teams []models.Team
	var total int64
	const cols = `id, organization_id, name, description, created_at, modified_at`

	if uc.IsSuperuser || uc.IsSystemAuditor {
		if err := h.DB.Select(&teams, `SELECT `+cols+` FROM teams ORDER BY id LIMIT $1 OFFSET $2`, pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		_ = h.DB.Get(&total, "SELECT count(*) FROM teams")
	} else {
		ids, err := h.readableIDs(r, rbac.ContentTypeTeam)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if len(ids) > 0 {
			q, args, _ := sqlx.In(`SELECT `+cols+` FROM teams WHERE id IN (?) ORDER BY id LIMIT ? OFFSET ?`, ids, pg.Limit, pg.Offset)
			q = h.DB.Rebind(q)
			if err := h.DB.Select(&teams, q, args...); err != nil {
				render.ErrInternal(err).Render(w, r)
				return
			}
			total = int64(len(ids))
		}
	}

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

	// A team must belong to an explicit organization — never silently default to
	// org 1, which would place resources in the wrong tenant.
	if input.OrganizationID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r) // organization_id is required
		return
	}

	// Creating a team requires admin on its parent organization.
	if !h.authorize(w, r, rbac.ContentTypeOrganization, input.OrganizationID, actAdmin) {
		return
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
		rows.Close()
		h.grantCreatorAdmin(r.Context(), rbac.ContentTypeTeam, created.ID, currentUser(r))
		render.Created(w, r, created)
	} else {
		render.ErrInternal(nil).Render(w, r)
	}
}

// GetTeam GET /api/v1/teams/{id}
func (h *ContentHandler) GetTeam(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if !h.authorize(w, r, rbac.ContentTypeTeam, id, actRead) {
		return
	}
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
	if !h.authorize(w, r, rbac.ContentTypeTeam, id, actAdmin) {
		return
	}
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
	if !h.authorize(w, r, rbac.ContentTypeTeam, id, actAdmin) {
		return
	}
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
	if !h.authorize(w, r, rbac.ContentTypeTeam, teamID, actAdmin) {
		return
	}

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
	if !h.authorize(w, r, rbac.ContentTypeTeam, teamID, actRead) {
		return
	}

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
	if !h.authorize(w, r, rbac.ContentTypeTeam, teamID, actAdmin) {
		return
	}

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
