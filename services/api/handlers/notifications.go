package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// NotificationStore is the notifications data access shared by the content and
// templates handlers.
type NotificationStore interface {
	ListTemplates(ctx context.Context, orgID int64) ([]store.NotificationTemplate, error)
	CreateTemplate(ctx context.Context, orgID int64, name, notificationType string, config []byte) (int64, error)
	TemplateOrg(ctx context.Context, id int64) (int64, error)
	DeleteTemplate(ctx context.Context, id int64) error
	JobTemplateAttachments(ctx context.Context, jobTemplateID int64) ([]store.JobTemplateNotification, error)
	AttachToJobTemplate(ctx context.Context, jobTemplateID, notificationTemplateID int64, event string) error
	DetachFromJobTemplate(ctx context.Context, jobTemplateID, notificationTemplateID int64, event string) error
}

// ListNotificationTemplates GET /api/v1/notification-templates?organization_id=N
func (h *ContentHandler) ListNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	orgID, err := strconv.ParseInt(r.URL.Query().Get("organization_id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(fmt.Errorf("organization_id is required")).Render(w, r)
		return
	}
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actRead) {
		return
	}
	nts, err := h.notifications.ListTemplates(r.Context(), orgID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, nts)
}

// CreateNotificationTemplate POST /api/v1/notification-templates
func (h *ContentHandler) CreateNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrganizationID   int64  `json:"organization_id"`
		Name             string `json:"name"`
		NotificationType string `json:"notification_type"`
		URL              string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.URL == "" {
		render.ErrInvalidRequest(fmt.Errorf("name, url required")).Render(w, r)
		return
	}
	if body.NotificationType == "" {
		body.NotificationType = "webhook"
	}
	if !h.authorize(w, r, rbac.ContentTypeOrganization, body.OrganizationID, actAdmin) {
		return
	}

	enc, err := crypto.EncryptSecret(body.URL)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	cfg, _ := json.Marshal(map[string]string{"url": enc})

	id, err := h.notifications.CreateTemplate(r.Context(), body.OrganizationID, body.Name, body.NotificationType, cfg)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"id": id})
}

// DeleteNotificationTemplate DELETE /api/v1/notification-templates/{id}
func (h *ContentHandler) DeleteNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	orgID, err := h.notifications.TemplateOrg(r.Context(), id)
	if err != nil {
		render.ErrInvalidRequest(fmt.Errorf("unknown notification template")).Render(w, r)
		return
	}
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actAdmin) {
		return
	}
	if err := h.notifications.DeleteTemplate(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListJobTemplateNotifications GET /api/v1/job-templates/{id}/notifications
func (rs *TemplatesResource) ListJobTemplateNotifications(w http.ResponseWriter, r *http.Request) {
	jtID := render.GetIDParam(r)
	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, jtID, actRead) {
		return
	}
	rows, err := rs.notifications.JobTemplateAttachments(r.Context(), jtID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}

// AttachJobTemplateNotification POST /api/v1/job-templates/{id}/notifications
func (rs *TemplatesResource) AttachJobTemplateNotification(w http.ResponseWriter, r *http.Request) {
	jtID := render.GetIDParam(r)
	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, jtID, actAdmin) {
		return
	}
	var body struct {
		NotificationTemplateID int64  `json:"notification_template_id"`
		Event                  string `json:"event"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NotificationTemplateID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	switch body.Event {
	case "started", "success", "error":
	default:
		render.ErrInvalidRequest(fmt.Errorf("event must be started|success|error")).Render(w, r)
		return
	}
	if err := rs.notifications.AttachToJobTemplate(r.Context(), jtID, body.NotificationTemplateID, body.Event); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DetachJobTemplateNotification DELETE /api/v1/job-templates/{id}/notifications/{ntId}/{event}
func (rs *TemplatesResource) DetachJobTemplateNotification(w http.ResponseWriter, r *http.Request) {
	jtID := render.GetIDParam(r)
	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, jtID, actAdmin) {
		return
	}
	ntID, err := strconv.ParseInt(chi.URLParam(r, "ntId"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	event := chi.URLParam(r, "event")
	if err := rs.notifications.DetachFromJobTemplate(r.Context(), jtID, ntID, event); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
