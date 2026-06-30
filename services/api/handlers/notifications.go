package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

func handlerSecretKey() string {
	if k := os.Getenv("PRAETOR_SECRET_KEY"); k != "" {
		return k
	}
	return "12345678901234567890123456789012"
}

// notificationTemplate is an org-scoped notification target. The config secret
// (the target URL) is stored encrypted and never returned to clients.
type notificationTemplate struct {
	ID               int64  `json:"id" db:"id"`
	OrganizationID   int64  `json:"organization_id" db:"organization_id"`
	Name             string `json:"name" db:"name"`
	NotificationType string `json:"notification_type" db:"notification_type"`
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
	nts := []notificationTemplate{}
	if err := h.DB.Select(&nts,
		`SELECT id, organization_id, name, notification_type FROM notification_templates
		 WHERE organization_id = $1 ORDER BY name`, orgID); err != nil {
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

	enc, err := crypto.Encrypt(body.URL, handlerSecretKey())
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	cfg, _ := json.Marshal(map[string]string{"url": enc})

	var id int64
	if err := h.DB.QueryRowx(
		`INSERT INTO notification_templates (organization_id, name, notification_type, config)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		body.OrganizationID, body.Name, body.NotificationType, cfg).Scan(&id); err != nil {
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
	var orgID int64
	if err := h.DB.Get(&orgID, `SELECT organization_id FROM notification_templates WHERE id = $1`, id); err != nil {
		render.ErrInvalidRequest(fmt.Errorf("unknown notification template")).Render(w, r)
		return
	}
	if !h.authorize(w, r, rbac.ContentTypeOrganization, orgID, actAdmin) {
		return
	}
	if _, err := h.DB.Exec(`DELETE FROM notification_templates WHERE id = $1`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// jobTemplateNotification is one attachment row (which notification fires on which event).
type jobTemplateNotification struct {
	NotificationTemplateID int64  `json:"notification_template_id" db:"notification_template_id"`
	Name                   string `json:"name" db:"name"`
	NotificationType       string `json:"notification_type" db:"notification_type"`
	Event                  string `json:"event" db:"event"`
}

// ListJobTemplateNotifications GET /api/v1/job-templates/{id}/notifications
func (rs *TemplatesResource) ListJobTemplateNotifications(w http.ResponseWriter, r *http.Request) {
	jtID := render.GetIDParam(r)
	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, jtID, actRead) {
		return
	}
	rows := []jobTemplateNotification{}
	if err := rs.DB.Select(&rows, `
		SELECT jtn.notification_template_id, nt.name, nt.notification_type, jtn.event
		FROM job_template_notifications jtn
		JOIN notification_templates nt ON nt.id = jtn.notification_template_id
		WHERE jtn.job_template_id = $1
		ORDER BY jtn.event, nt.name`, jtID); err != nil {
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
	if _, err := rs.DB.Exec(`
		INSERT INTO job_template_notifications (job_template_id, notification_template_id, event)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		jtID, body.NotificationTemplateID, body.Event); err != nil {
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
	if _, err := rs.DB.Exec(
		`DELETE FROM job_template_notifications WHERE job_template_id=$1 AND notification_template_id=$2 AND event=$3`,
		jtID, ntID, event); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
