package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
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

	if uc.IsSuperuser || uc.IsSystemAuditor {
		var err error
		if teams, err = h.teams.ListAll(r.Context(), pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total, _ = h.teams.CountAll(r.Context())
	} else {
		ids, err := h.readableIDs(r, rbac.ContentTypeTeam)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if teams, err = h.teams.ListByIDs(r.Context(), ids, pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total = int64(len(ids))
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

	created, err := h.teams.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	h.grantCreatorAdmin(r.Context(), rbac.ContentTypeTeam, created.ID, currentUser(r))
	render.Created(w, r, created)
}

// GetTeam GET /api/v1/teams/{id}
func (h *ContentHandler) GetTeam(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if !h.authorize(w, r, rbac.ContentTypeTeam, id, actRead) {
		return
	}
	team, err := h.teams.Get(r.Context(), id)
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

	updated, err := h.teams.Update(r.Context(), input)
	if errors.Is(err, sql.ErrNoRows) {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	} else if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, updated)
}

// DeleteTeam DELETE /api/v1/teams/{id}
func (h *ContentHandler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if !h.authorize(w, r, rbac.ContentTypeTeam, id, actAdmin) {
		return
	}
	count, err := h.teams.Delete(r.Context(), id)
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

	if err := h.teams.AddMember(r.Context(), teamID, req.UserID); err != nil {
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

	members, err := h.teams.Members(r.Context(), teamID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
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

	if err := h.teams.RemoveMember(r.Context(), teamID, userID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.NoContent(w, r)
}
