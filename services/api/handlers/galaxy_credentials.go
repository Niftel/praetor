package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

// ListOrgGalaxyCredentials GET /api/v1/organizations/{id}/galaxy-credentials
func (h *ContentHandler) ListOrgGalaxyCredentials(w http.ResponseWriter, r *http.Request) {
	orgID := render.GetIDParam(r)
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actRead) {
		return
	}

	creds, err := h.orgs.GalaxyCredentials(r.Context(), orgID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, creds)
}

// AddOrgGalaxyCredential POST /api/v1/organizations/{id}/galaxy-credentials
func (h *ContentHandler) AddOrgGalaxyCredential(w http.ResponseWriter, r *http.Request) {
	orgID := render.GetIDParam(r)
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actAdmin) {
		return
	}

	var body struct {
		CredentialID int64 `json:"credential_id"`
		Position     int   `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CredentialID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	if err := h.orgs.AddGalaxyCredential(r.Context(), orgID, body.CredentialID, body.Position); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveOrgGalaxyCredential DELETE /api/v1/organizations/{id}/galaxy-credentials/{credId}
func (h *ContentHandler) RemoveOrgGalaxyCredential(w http.ResponseWriter, r *http.Request) {
	orgID := render.GetIDParam(r)
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actAdmin) {
		return
	}
	credID, err := strconv.ParseInt(chi.URLParam(r, "credId"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if err := h.orgs.RemoveGalaxyCredential(r.Context(), orgID, credID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
