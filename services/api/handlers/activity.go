package handlers

import (
	"net/http"
	"strconv"

	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/render"
)

// ListActivityStream GET /api/v1/activity-stream?limit=&resource_type=&action=
// The audit log is sensitive (every user's actions), so it's restricted to the
// global view_activitystream capability (System Administrator and System Auditor).
func (h *AccessResource) ListActivityStream(w http.ResponseWriter, r *http.Request) {
	if !h.requireGlobal(w, r, rbac.ViewActivityStream) {
		return
	}

	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}

	entries, err := h.store.ActivityStream(r.Context(),
		r.URL.Query().Get("resource_type"), r.URL.Query().Get("action"), limit)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, entries)
}
