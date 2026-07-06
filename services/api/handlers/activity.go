package handlers

import (
	"net/http"
	"strconv"

	"github.com/praetordev/praetor/services/api/render"
)

// ListActivityStream GET /api/v1/activity-stream?limit=&resource_type=&action=
// The audit log is sensitive (every user's actions), so it's restricted to
// superusers and system auditors.
func (h *ContentHandler) ListActivityStream(w http.ResponseWriter, r *http.Request) {
	uc := currentUser(r)
	if !uc.IsSuperuser && !uc.IsSystemAuditor {
		render.ErrForbidden(nil).Render(w, r)
		return
	}

	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}

	entries, err := h.access.ActivityStream(r.Context(),
		r.URL.Query().Get("resource_type"), r.URL.Query().Get("action"), limit)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, entries)
}
