package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/praetordev/praetor/services/api/render"
)

type activityEntry struct {
	ID           int64     `json:"id" db:"id"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UserID       *int64    `json:"user_id" db:"user_id"`
	Username     string    `json:"username" db:"username"`
	Action       string    `json:"action" db:"action"`
	ResourceType string    `json:"resource_type" db:"resource_type"`
	ResourceID   *int64    `json:"resource_id" db:"resource_id"`
	Method       string    `json:"method" db:"method"`
	Path         string    `json:"path" db:"path"`
	StatusCode   int       `json:"status_code" db:"status_code"`
}

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

	query := `SELECT id, created_at, user_id, username, action, resource_type, resource_id, method, path, status_code
	          FROM activity_stream WHERE 1=1`
	args := []interface{}{}
	if rt := r.URL.Query().Get("resource_type"); rt != "" {
		args = append(args, rt)
		query += " AND resource_type = $" + strconv.Itoa(len(args))
	}
	if act := r.URL.Query().Get("action"); act != "" {
		args = append(args, act)
		query += " AND action = $" + strconv.Itoa(len(args))
	}
	args = append(args, limit)
	query += " ORDER BY created_at DESC LIMIT $" + strconv.Itoa(len(args))

	entries := []activityEntry{}
	if err := h.DB.SelectContext(r.Context(), &entries, query, args...); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, entries)
}
