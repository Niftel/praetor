package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/notify"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
)

// NotificationStore is the notifications data access shared by the notifications
// and templates handlers.
type NotificationStore interface {
	ListTemplates(ctx context.Context, orgID int64) ([]store.NotificationTemplate, error)
	CreateTemplate(ctx context.Context, orgID int64, name, notificationType string, config []byte) (int64, error)
	TemplateOrg(ctx context.Context, id int64) (int64, error)
	DeleteTemplate(ctx context.Context, id int64) error
	JobTemplateAttachments(ctx context.Context, jobTemplateID int64) ([]store.JobTemplateNotification, error)
	AttachToJobTemplate(ctx context.Context, jobTemplateID, notificationTemplateID int64, event string) error
	DetachFromJobTemplate(ctx context.Context, jobTemplateID, notificationTemplateID int64, event string) error
	WorkflowTemplateAttachments(ctx context.Context, workflowTemplateID int64) ([]store.JobTemplateNotification, error)
	AttachToWorkflowTemplate(ctx context.Context, workflowTemplateID, notificationTemplateID int64, event string) error
	DetachFromWorkflowTemplate(ctx context.Context, workflowTemplateID, notificationTemplateID int64, event string) error
}

// NotificationsResource is the self-contained notification-templates domain
// (extracted from the former ContentHandler god-object — B6/#85). Job-template
// attachment endpoints live on TemplatesResource; the org-scoped targets live
// here. Embeds *Authorizer for the shared RBAC helpers.
type NotificationsResource struct {
	DB *sqlx.DB
	*Authorizer
	store NotificationStore
}

func NewNotificationsResource(db *sqlx.DB, authz *Authorizer) *NotificationsResource {
	return &NotificationsResource{DB: db, Authorizer: authz, store: store.NewNotificationStore(db)}
}

// ListNotificationTemplates GET /api/v1/notification-templates?organization_id=N
func (h *NotificationsResource) ListNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	orgID, err := strconv.ParseInt(r.URL.Query().Get("organization_id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(fmt.Errorf("organization_id is required")).Render(w, r)
		return
	}
	if !h.authorize(w, r, rbac.Organization, orgID, actRead) {
		return
	}
	nts, err := h.store.ListTemplates(r.Context(), orgID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, nts)
}

// ListNotificationTypes GET /api/v1/notification-types — the registered backends
// and their config schemas, so the UI renders the create-form dynamically (and
// so "which types exist" has a source of truth instead of a migration comment).
func (h *NotificationsResource) ListNotificationTypes(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]interface{}, 0)
	for _, name := range notify.Backends.Names() {
		b, _ := notify.Backends.Get(name)
		out = append(out, map[string]interface{}{"type": name, "fields": b.ConfigFields()})
	}
	render.JSON(w, r, out)
}

// CreateNotificationTemplate POST /api/v1/notification-templates. Config is
// validated and encrypted against the selected backend's schema. Accepts either
// a typed `config` map or, for backward compatibility with the current UI, a
// bare `url` (mapped to {"url": ...}).
func (h *NotificationsResource) CreateNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrganizationID   int64             `json:"organization_id"`
		Name             string            `json:"name"`
		NotificationType string            `json:"notification_type"`
		Config           map[string]string `json:"config"`
		URL              string            `json:"url"` // legacy shorthand for config.url
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" || body.OrganizationID <= 0 {
		render.ErrInvalidRequest(fmt.Errorf("name required")).Render(w, r)
		return
	}
	if body.NotificationType == "" {
		body.NotificationType = "webhook"
	}
	if body.Config == nil {
		body.Config = map[string]string{}
	}
	if body.URL != "" {
		body.Config["url"] = body.URL // back-compat
	}
	if !h.authorizeOrgRole(w, r, body.OrganizationID, rbac.NotificationAdminRole) {
		return
	}

	backend, ok := notify.Backends.Get(body.NotificationType)
	if !ok {
		render.ErrInvalidRequest(fmt.Errorf("unknown notification type %q", body.NotificationType)).Render(w, r)
		return
	}
	for _, field := range backend.ConfigFields() {
		if field.ID != "url" {
			continue
		}
		if err := notify.ValidateDestination(r.Context(), body.Config[field.ID]); err != nil {
			render.ErrInvalidRequest(err).Render(w, r)
			return
		}
	}
	cfg, err := notify.EncryptConfig(backend, body.Config)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	id, err := h.store.CreateTemplate(r.Context(), body.OrganizationID, strings.TrimSpace(body.Name), body.NotificationType, cfg)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"id": id})
}

type notificationDeliveryTarget struct {
	ID               int64           `db:"id"`
	OrganizationID   int64           `db:"organization_id"`
	Name             string          `db:"name"`
	NotificationType string          `db:"notification_type"`
	Config           json.RawMessage `db:"config"`
}

// TestNotificationTemplate POST /api/v1/notification-templates/{id}/test
// sends a bounded synthetic message through the same decrypt-and-deliver path
// used by lifecycle notifications. Stored config remains server-side and is
// never serialized into the response.
func (h *NotificationsResource) TestNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		render.ErrInvalidRequest(fmt.Errorf("invalid notification template id")).Render(w, r)
		return
	}
	var target notificationDeliveryTarget
	if err := h.DB.GetContext(r.Context(), &target, `
		SELECT id, organization_id, name, notification_type, config
		FROM notification_templates WHERE id = $1`, id); err != nil {
		if err == sql.ErrNoRows {
			render.ErrNotFound(err).Render(w, r)
			return
		}
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !h.authorizeOrgRole(w, r, target.OrganizationID, rbac.NotificationAdminRole) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err = notify.SendOne(ctx, target.NotificationType, target.Config, notify.Message{
		JobID:   target.ID,
		JobName: target.Name,
		Event:   "test",
		Status:  "test notification delivered",
		Kind:    "notification target",
	})
	if err != nil {
		failureCode := "delivery_failed"
		if ctx.Err() != nil {
			failureCode = "delivery_timeout"
		}
		// Delivery errors can contain a webhook URL whose path embeds a secret.
		// Record only bounded identifiers and a stable failure code.
		logger.Warn("notification test delivery failed", "notification_template_id", target.ID, "organization_id", target.OrganizationID, "notification_type", target.NotificationType, "failure_code", failureCode)
		(&render.ErrorResponse{Err: err, HTTPStatusCode: http.StatusBadGateway, ErrorText: "Test notification could not be delivered (" + failureCode + ")"}).Render(w, r)
		return
	}
	render.JSON(w, r, map[string]interface{}{
		"status":                   "delivered",
		"notification_template_id": target.ID,
		"tested_at":                time.Now().UTC(),
	})
}

// DeleteNotificationTemplate DELETE /api/v1/notification-templates/{id}
func (h *NotificationsResource) DeleteNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	orgID, err := h.store.TemplateOrg(r.Context(), id)
	if err != nil {
		render.ErrInvalidRequest(fmt.Errorf("unknown notification template")).Render(w, r)
		return
	}
	if !h.authorizeOrgRole(w, r, orgID, rbac.NotificationAdminRole) {
		return
	}
	if err := h.store.DeleteTemplate(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListJobTemplateNotifications GET /api/v1/job-templates/{id}/notifications
func (rs *TemplatesResource) ListJobTemplateNotifications(w http.ResponseWriter, r *http.Request) {
	jtID := render.GetIDParam(r)
	if !rs.authorize(w, r, rbac.JobTemplate, jtID, actRead) {
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
	if !rs.authorize(w, r, rbac.JobTemplate, jtID, actAdmin) {
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
	if !rs.authorize(w, r, rbac.JobTemplate, jtID, actAdmin) {
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

// ListWorkflowNotifications GET /api/v1/workflow-templates/{id}/notifications
func (rs *WorkflowsResource) ListWorkflowNotifications(w http.ResponseWriter, r *http.Request) {
	wtID := render.GetIDParam(r)
	if !rs.authorize(w, r, rbac.WorkflowTemplate, wtID, actRead) {
		return
	}
	rows, err := rs.notifications.WorkflowTemplateAttachments(r.Context(), wtID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}

// AttachWorkflowNotification POST /api/v1/workflow-templates/{id}/notifications.
// A workflow fires 'started' on first advance, 'success'/'error' on terminal
// state, 'approval' when an approval node starts waiting, 'approved'/'denied'
// on a human outcome, and 'timeout' when the scheduler expires a gate.
func (rs *WorkflowsResource) AttachWorkflowNotification(w http.ResponseWriter, r *http.Request) {
	wtID := render.GetIDParam(r)
	if !rs.authorize(w, r, rbac.WorkflowTemplate, wtID, actAdmin) {
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
	case "started", "success", "error", "approval", "approved", "denied", "timeout":
	default:
		render.ErrInvalidRequest(fmt.Errorf("event must be started|success|error|approval|approved|denied|timeout")).Render(w, r)
		return
	}
	if err := rs.notifications.AttachToWorkflowTemplate(r.Context(), wtID, body.NotificationTemplateID, body.Event); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DetachWorkflowNotification DELETE /api/v1/workflow-templates/{id}/notifications/{ntId}/{event}
func (rs *WorkflowsResource) DetachWorkflowNotification(w http.ResponseWriter, r *http.Request) {
	wtID := render.GetIDParam(r)
	if !rs.authorize(w, r, rbac.WorkflowTemplate, wtID, actAdmin) {
		return
	}
	ntID, err := strconv.ParseInt(chi.URLParam(r, "ntId"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	event := chi.URLParam(r, "event")
	if err := rs.notifications.DetachFromWorkflowTemplate(r.Context(), wtID, ntID, event); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
