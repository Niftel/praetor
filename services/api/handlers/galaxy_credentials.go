package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

// orgGalaxyCredential is a Galaxy credential attached to an organization.
type orgGalaxyCredential struct {
	ID           int64  `json:"id" db:"id"`
	CredentialID int64  `json:"credential_id" db:"credential_id"`
	Name         string `json:"name" db:"name"`
	Position     int    `json:"position" db:"position"`
}

// ListOrgGalaxyCredentials GET /api/v1/organizations/{id}/galaxy-credentials
func (h *ContentHandler) ListOrgGalaxyCredentials(w http.ResponseWriter, r *http.Request) {
	orgID := render.GetIDParam(r)
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actRead) {
		return
	}

	creds := []orgGalaxyCredential{}
	err := h.DB.Select(&creds, `
		SELECT ogc.id, ogc.credential_id, c.name, ogc.position
		FROM organization_galaxy_credentials ogc
		JOIN credentials c ON c.id = ogc.credential_id
		WHERE ogc.organization_id = $1
		ORDER BY ogc.position, ogc.id`, orgID)
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

	if _, err := h.DB.Exec(`
		INSERT INTO organization_galaxy_credentials (organization_id, credential_id, position)
		VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, credential_id) DO UPDATE SET position = EXCLUDED.position`,
		orgID, body.CredentialID, body.Position); err != nil {
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

	if _, err := h.DB.Exec(
		`DELETE FROM organization_galaxy_credentials WHERE organization_id = $1 AND credential_id = $2`,
		orgID, credID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
