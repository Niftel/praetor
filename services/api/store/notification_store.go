package store

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// NotificationTemplate is an org-scoped notification target (config secret never
// returned).
type NotificationTemplate struct {
	ID               int64  `json:"id" db:"id"`
	OrganizationID   int64  `json:"organization_id" db:"organization_id"`
	Name             string `json:"name" db:"name"`
	NotificationType string `json:"notification_type" db:"notification_type"`
}

// JobTemplateNotification is one attachment (which notification fires on which event).
type JobTemplateNotification struct {
	NotificationTemplateID int64  `json:"notification_template_id" db:"notification_template_id"`
	Name                   string `json:"name" db:"name"`
	NotificationType       string `json:"notification_type" db:"notification_type"`
	Event                  string `json:"event" db:"event"`
}

// NotificationStore is the data-access layer for notification templates and their
// job-template attachments.
type NotificationStore struct {
	db *sqlx.DB
}

func NewNotificationStore(db *sqlx.DB) *NotificationStore { return &NotificationStore{db: db} }

// ListTemplates returns an org's notification templates.
func (s *NotificationStore) ListTemplates(ctx context.Context, orgID int64) ([]NotificationTemplate, error) {
	nts := []NotificationTemplate{}
	err := s.db.SelectContext(ctx, &nts,
		`SELECT id, organization_id, name, notification_type FROM notification_templates
		 WHERE organization_id = $1 ORDER BY name`, orgID)
	return nts, wrap("NotificationStore.ListTemplates", err)
}

// CreateTemplate inserts a notification template (config already encrypted) and
// returns its id.
func (s *NotificationStore) CreateTemplate(ctx context.Context, orgID int64, name, notificationType string, config []byte) (int64, error) {
	var id int64
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO notification_templates (organization_id, name, notification_type, config)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		orgID, name, notificationType, config).Scan(&id)
	return id, wrap("NotificationStore.CreateTemplate", err)
}

// TemplateOrg returns the org that owns a notification template.
func (s *NotificationStore) TemplateOrg(ctx context.Context, id int64) (int64, error) {
	var orgID int64
	err := s.db.GetContext(ctx, &orgID, `SELECT organization_id FROM notification_templates WHERE id = $1`, id)
	return orgID, wrap("NotificationStore.TemplateOrg", err)
}

// DeleteTemplate removes a notification template.
func (s *NotificationStore) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_templates WHERE id = $1`, id)
	return wrap("NotificationStore.DeleteTemplate", err)
}

// JobTemplateAttachments lists the notifications attached to a job template.
func (s *NotificationStore) JobTemplateAttachments(ctx context.Context, jobTemplateID int64) ([]JobTemplateNotification, error) {
	rows := []JobTemplateNotification{}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT jtn.notification_template_id, nt.name, nt.notification_type, jtn.event
		FROM job_template_notifications jtn
		JOIN notification_templates nt ON nt.id = jtn.notification_template_id
		WHERE jtn.job_template_id = $1
		ORDER BY jtn.event, nt.name`, jobTemplateID)
	return rows, wrap("NotificationStore.JobTemplateAttachments", err)
}

// AttachToJobTemplate attaches a notification to a job template for an event (idempotent).
func (s *NotificationStore) AttachToJobTemplate(ctx context.Context, jobTemplateID, notificationTemplateID int64, event string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_template_notifications (job_template_id, notification_template_id, event)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, jobTemplateID, notificationTemplateID, event)
	return wrap("NotificationStore.AttachToJobTemplate", err)
}

// DetachFromJobTemplate removes a notification attachment.
func (s *NotificationStore) DetachFromJobTemplate(ctx context.Context, jobTemplateID, notificationTemplateID int64, event string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM job_template_notifications WHERE job_template_id=$1 AND notification_template_id=$2 AND event=$3`,
		jobTemplateID, notificationTemplateID, event)
	return wrap("NotificationStore.DetachFromJobTemplate", err)
}

// WorkflowTemplateAttachments lists the notifications attached to a workflow
// template (same row shape as job-template attachments).
func (s *NotificationStore) WorkflowTemplateAttachments(ctx context.Context, workflowTemplateID int64) ([]JobTemplateNotification, error) {
	rows := []JobTemplateNotification{}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT wtn.notification_template_id, nt.name, nt.notification_type, wtn.event
		FROM workflow_template_notifications wtn
		JOIN notification_templates nt ON nt.id = wtn.notification_template_id
		WHERE wtn.workflow_template_id = $1
		ORDER BY wtn.event, nt.name`, workflowTemplateID)
	return rows, wrap("NotificationStore.WorkflowTemplateAttachments", err)
}

// AttachToWorkflowTemplate attaches a notification to a workflow template for an
// event (idempotent).
func (s *NotificationStore) AttachToWorkflowTemplate(ctx context.Context, workflowTemplateID, notificationTemplateID int64, event string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_template_notifications (workflow_template_id, notification_template_id, event)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, workflowTemplateID, notificationTemplateID, event)
	return wrap("NotificationStore.AttachToWorkflowTemplate", err)
}

// DetachFromWorkflowTemplate removes a workflow-template notification attachment.
func (s *NotificationStore) DetachFromWorkflowTemplate(ctx context.Context, workflowTemplateID, notificationTemplateID int64, event string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM workflow_template_notifications WHERE workflow_template_id=$1 AND notification_template_id=$2 AND event=$3`,
		workflowTemplateID, notificationTemplateID, event)
	return wrap("NotificationStore.DetachFromWorkflowTemplate", err)
}
