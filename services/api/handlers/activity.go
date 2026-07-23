package handlers

import (
	"net/http"
	"strconv"
	"time"

	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/render"
)

type activityEntry struct {
	ID                  int64     `json:"id" db:"id"`
	CreatedAt           time.Time `json:"created_at" db:"created_at"`
	UserID              *int64    `json:"user_id,omitempty" db:"user_id"`
	Username            string    `json:"username" db:"username"`
	PrincipalKind       string    `json:"principal_kind" db:"principal_kind"`
	ServicePrincipalID  *int64    `json:"service_principal_id,omitempty" db:"service_principal_id"`
	ServiceCredentialID *int64    `json:"service_credential_id,omitempty" db:"service_credential_id"`
	Action              string    `json:"action" db:"action"`
	ResourceType        string    `json:"resource_type" db:"resource_type"`
	ResourceID          *int64    `json:"resource_id,omitempty" db:"resource_id"`
	OrganizationID      *int64    `json:"organization_id,omitempty" db:"organization_id"`
	Method              string    `json:"method" db:"method"`
	Path                string    `json:"path" db:"path"`
	StatusCode          int       `json:"status_code" db:"status_code"`
	Outcome             string    `json:"outcome" db:"outcome"`
	FailureCode         *string   `json:"failure_code,omitempty" db:"failure_code"`
}

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

	resourceType := r.URL.Query().Get("resource_type")
	action := r.URL.Query().Get("action")
	entries := []activityEntry{}
	err := h.DB.SelectContext(r.Context(), &entries, `
		SELECT id, created_at, user_id, username, principal_kind,
		       service_principal_id, service_credential_id, action,
		       resource_type, resource_id, organization_id, method, path,
		       status_code, outcome, failure_code
		  FROM activity_stream
		 WHERE ($1 = '' OR resource_type=$1)
		   AND ($2 = '' OR action=$2)
		 ORDER BY created_at DESC
		 LIMIT $3`, resourceType, action, limit)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, entries)
}
