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
	modelAuth "github.com/praetordev/praetor/services/api/middleware"
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

func auditNotification(r *http.Request, organizationID, resourceID int64, resourceType, action, failureCode string) {
	modelAuth.SetActivityMetadata(r, modelAuth.ActivityMetadata{
		OrganizationID: organizationID,
		ResourceID:     resourceID,
		ResourceType:   resourceType,
		Action:         action,
		FailureCode:    failureCode,
	})
}

func auditNotificationAttachment(db *sqlx.DB, r *http.Request, automationType string, automationID int64, action string) {
	var organizationID int64
	var err error
	switch automationType {
	case "job_template":
		err = db.GetContext(r.Context(), &organizationID,
			`SELECT organization_id FROM job_templates WHERE id=$1`, automationID)
	case "workflow_template":
		err = db.GetContext(r.Context(), &organizationID,
			`SELECT organization_id FROM workflow_templates WHERE id=$1`, automationID)
	default:
		return
	}
	if err != nil {
		return
	}
	auditNotification(r, organizationID, automationID, "notification_policy", action, "")
}

func (h *NotificationsResource) authorizeNotificationAdmin(w http.ResponseWriter, r *http.Request, organizationID int64) bool {
	auditNotification(r, organizationID, 0, "", "", "")
	allowed, err := h.canAuthorize(r, rbac.Organization, organizationID, actAdmin)
	if err != nil {
		auditNotification(r, organizationID, 0, "", "", "authorization_error")
		renderNotificationError(w, r, render.ErrInternal(err))
		return false
	}
	if !allowed {
		auditNotification(r, organizationID, 0, "", "", "notification_admin_required")
		renderNotificationError(w, r, &render.ErrorResponse{
			HTTPStatusCode: http.StatusForbidden,
			ErrorText:      "Notification administration denied (notification_admin_required)",
		})
		return false
	}
	return true
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
	auditNotification(r, 0, 0, "notification_template", "create", "")
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
	if !h.authorizeNotificationAdmin(w, r, body.OrganizationID) {
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
			renderNotificationError(w, r, render.ErrInvalidRequest(err))
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
	auditNotification(r, body.OrganizationID, id, "notification_template", "create", "")
	render.Created(w, r, map[string]interface{}{"id": id})
}

type notificationDeliveryTarget struct {
	ID               int64           `db:"id"`
	OrganizationID   int64           `db:"organization_id"`
	Name             string          `db:"name"`
	NotificationType string          `db:"notification_type"`
	Config           json.RawMessage `db:"config"`
}

type notificationPolicy struct {
	ID                     int64   `db:"id" json:"id"`
	OrganizationID         int64   `db:"organization_id" json:"organization_id"`
	TeamID                 *int64  `db:"team_id" json:"team_id,omitempty"`
	TeamName               *string `db:"team_name" json:"team_name,omitempty"`
	NotificationTemplateID int64   `db:"notification_template_id" json:"notification_template_id"`
	NotificationName       string  `db:"notification_name" json:"notification_name"`
	NotificationType       string  `db:"notification_type" json:"notification_type"`
	ResourceType           string  `db:"resource_type" json:"resource_type"`
	ResourceID             int64   `db:"resource_id" json:"resource_id"`
	Event                  string  `db:"event" json:"event"`
}

type notificationDeliveryAttempt struct {
	AttemptNumber int16     `json:"attempt_number"`
	Outcome       string    `json:"outcome"`
	FailureCode   *string   `json:"failure_code,omitempty"`
	FailureReason *string   `json:"failure_reason,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
}

type notificationDeliveryHistory struct {
	ID                     int64                         `db:"id" json:"id"`
	OrganizationID         int64                         `db:"organization_id" json:"organization_id"`
	TeamID                 *int64                        `db:"team_id" json:"team_id,omitempty"`
	TeamName               *string                       `db:"team_name" json:"team_name,omitempty"`
	NotificationTemplateID *int64                        `db:"notification_template_id" json:"notification_template_id,omitempty"`
	TargetName             string                        `db:"target_name" json:"target_name"`
	TargetType             string                        `db:"target_type" json:"target_type"`
	ResourceType           string                        `db:"resource_type" json:"resource_type"`
	ResourceID             int64                         `db:"resource_id" json:"resource_id"`
	Event                  string                        `db:"event" json:"event"`
	OccurrenceType         string                        `db:"occurrence_type" json:"occurrence_type"`
	OccurrenceID           string                        `db:"occurrence_id" json:"occurrence_id"`
	SubjectID              int64                         `db:"subject_id" json:"subject_id"`
	SubjectName            string                        `db:"subject_name" json:"subject_name"`
	SubjectKind            string                        `db:"subject_kind" json:"subject_kind"`
	Status                 string                        `db:"status" json:"status"`
	AttemptCount           int16                         `db:"attempt_count" json:"attempt_count"`
	MaxAttempts            int16                         `db:"max_attempts" json:"max_attempts"`
	NextAttemptAt          time.Time                     `db:"next_attempt_at" json:"next_attempt_at"`
	FirstAttemptAt         *time.Time                    `db:"first_attempt_at" json:"first_attempt_at,omitempty"`
	LastAttemptAt          *time.Time                    `db:"last_attempt_at" json:"last_attempt_at,omitempty"`
	DeliveredAt            *time.Time                    `db:"delivered_at" json:"delivered_at,omitempty"`
	FailedAt               *time.Time                    `db:"failed_at" json:"failed_at,omitempty"`
	FailureCode            *string                       `db:"failure_code" json:"failure_code,omitempty"`
	FailureReason          *string                       `db:"failure_reason" json:"failure_reason,omitempty"`
	CreatedAt              time.Time                     `db:"created_at" json:"created_at"`
	UpdatedAt              time.Time                     `db:"updated_at" json:"updated_at"`
	AttemptsJSON           json.RawMessage               `db:"attempts" json:"-"`
	Attempts               []notificationDeliveryAttempt `db:"-" json:"attempts"`
}

type notificationDeliveryHistoryResponse struct {
	Results    []notificationDeliveryHistory `json:"results"`
	NextCursor *int64                        `json:"next_cursor,omitempty"`
}

type notificationDeliveryHistoryFilter struct {
	OrganizationID int64
	Cursor         int64
	Limit          int
	Status         string
}

func parseNotificationDeliveryHistoryFilter(r *http.Request) (notificationDeliveryHistoryFilter, error) {
	filter := notificationDeliveryHistoryFilter{Limit: 25, Status: strings.TrimSpace(r.URL.Query().Get("status"))}
	var err error
	filter.OrganizationID, err = strconv.ParseInt(r.URL.Query().Get("organization_id"), 10, 64)
	if err != nil || filter.OrganizationID <= 0 {
		return filter, fmt.Errorf("organization_id is required")
	}
	if filter.Status != "" && filter.Status != "pending" && filter.Status != "retrying" && filter.Status != "sending" &&
		filter.Status != "delivered" && filter.Status != "failed" {
		return filter, fmt.Errorf("unsupported notification delivery status %q", filter.Status)
	}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		filter.Cursor, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || filter.Cursor <= 0 {
			return filter, fmt.Errorf("cursor must be a positive delivery id")
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		filter.Limit, err = strconv.Atoi(raw)
		if err != nil || filter.Limit <= 0 || filter.Limit > 100 {
			return filter, fmt.Errorf("limit must be between 1 and 100")
		}
	}
	return filter, nil
}

type notificationPolicyResource struct {
	OrganizationID int64
	ContentType    rbac.ResourceKind
	ObjectID       int64
}

func (h *NotificationsResource) resolvePolicyResource(ctx context.Context, resourceType string, resourceID int64) (notificationPolicyResource, error) {
	var resource notificationPolicyResource
	switch resourceType {
	case "job_template":
		resource.ContentType = rbac.JobTemplate
		resource.ObjectID = resourceID
		if err := h.DB.GetContext(ctx, &resource.OrganizationID, `SELECT organization_id FROM job_templates WHERE id=$1`, resourceID); err != nil {
			return resource, err
		}
	case "workflow_template":
		resource.ContentType = rbac.WorkflowTemplate
		resource.ObjectID = resourceID
		if err := h.DB.GetContext(ctx, &resource.OrganizationID, `SELECT organization_id FROM workflow_templates WHERE id=$1`, resourceID); err != nil {
			return resource, err
		}
	case "inventory_source":
		resource.ContentType = rbac.Inventory
		if err := h.DB.QueryRowxContext(ctx, `
			SELECT i.organization_id, i.id
			  FROM inventory_sources src
			  JOIN inventories i ON i.id=src.inventory_id
			 WHERE src.id=$1`, resourceID).Scan(&resource.OrganizationID, &resource.ObjectID); err != nil {
			return resource, err
		}
	default:
		return resource, fmt.Errorf("unsupported notification policy resource type %q", resourceType)
	}
	return resource, nil
}

// ListNotificationDeliveryHistory GET /api/v1/notification-deliveries.
//
// Organization administrators can inspect every delivery in the organization.
// Other organization readers see only deliveries explicitly scoped to teams
// they belong to. Target configuration and idempotency keys are intentionally
// absent from both the SELECT and response.
func (h *NotificationsResource) ListNotificationDeliveryHistory(w http.ResponseWriter, r *http.Request) {
	filter, err := parseNotificationDeliveryHistoryFilter(r)
	if err != nil {
		renderNotificationError(w, r, render.ErrInvalidRequest(err))
		return
	}
	if !h.authorize(w, r, rbac.Organization, filter.OrganizationID, actRead) {
		return
	}
	canViewAll, err := h.canAuthorize(r, rbac.Organization, filter.OrganizationID, actAdmin)
	if err != nil {
		renderNotificationError(w, r, render.ErrInternal(err))
		return
	}

	rows := []notificationDeliveryHistory{}
	if err := h.DB.SelectContext(r.Context(), &rows, `
		SELECT d.id, d.organization_id, d.team_id, team.name AS team_name,
		       d.notification_template_id, d.target_name, d.target_type,
		       d.resource_type, d.resource_id, d.event,
		       d.occurrence_type, d.occurrence_id,
		       d.subject_id, d.subject_name, d.subject_kind,
		       d.status, d.attempt_count, d.max_attempts, d.next_attempt_at,
		       d.first_attempt_at, d.last_attempt_at, d.delivered_at, d.failed_at,
		       d.failure_code, d.failure_reason, d.created_at, d.updated_at,
		       COALESCE((
		           SELECT jsonb_agg(jsonb_build_object(
		               'attempt_number', a.attempt_number,
		               'outcome', a.outcome,
		               'failure_code', a.failure_code,
		               'failure_reason', a.failure_reason,
		               'started_at', a.started_at,
		               'finished_at', a.finished_at
		           ) ORDER BY a.attempt_number)
		             FROM notification_delivery_attempts a
		            WHERE a.delivery_id=d.id
		       ), '[]'::jsonb) AS attempts
		  FROM notification_deliveries d
		  LEFT JOIN teams team ON team.id=d.team_id
		 WHERE d.organization_id=$1
		   AND ($2 OR (
		       d.team_id IS NOT NULL
		       AND EXISTS (
		           SELECT 1 FROM team_members tm
		            WHERE tm.team_id=d.team_id AND tm.user_id=$3
		       )
		   ))
		   AND ($4::bigint = 0 OR d.id < $4)
		   AND ($5 = '' OR d.status=$5)
		 ORDER BY d.id DESC
		 LIMIT $6`,
		filter.OrganizationID, canViewAll, currentUser(r).UserID,
		filter.Cursor, filter.Status, filter.Limit+1); err != nil {
		renderNotificationError(w, r, render.ErrInternal(err))
		return
	}

	response := notificationDeliveryHistoryResponse{Results: rows}
	if len(rows) > filter.Limit {
		response.Results = rows[:filter.Limit]
		next := response.Results[len(response.Results)-1].ID
		response.NextCursor = &next
	}
	for i := range response.Results {
		if err := json.Unmarshal(response.Results[i].AttemptsJSON, &response.Results[i].Attempts); err != nil {
			renderNotificationError(w, r, render.ErrInternal(fmt.Errorf("decode notification delivery attempts: %w", err)))
			return
		}
	}
	render.JSON(w, r, response)
}

// ListNotificationPolicies GET /api/v1/notification-policies?resource_type=...&resource_id=N
// requires visibility of both the automation resource and its organization.
// Target configuration is never selected by this endpoint.
func (h *NotificationsResource) ListNotificationPolicies(w http.ResponseWriter, r *http.Request) {
	resourceID, err := strconv.ParseInt(r.URL.Query().Get("resource_id"), 10, 64)
	if err != nil || resourceID <= 0 {
		renderNotificationError(w, r, render.ErrInvalidRequest(fmt.Errorf("resource_id is required")))
		return
	}
	resourceType := r.URL.Query().Get("resource_type")
	resource, err := h.resolvePolicyResource(r.Context(), resourceType, resourceID)
	if err != nil {
		renderNotificationError(w, r, render.ErrInvalidRequest(fmt.Errorf("unknown notification policy resource")))
		return
	}
	if !h.authorize(w, r, resource.ContentType, resource.ObjectID, actRead) ||
		!h.authorize(w, r, rbac.Organization, resource.OrganizationID, actRead) {
		return
	}

	policies := []notificationPolicy{}
	if err := h.DB.SelectContext(r.Context(), &policies, `
		SELECT p.id, p.organization_id, p.team_id, team.name AS team_name,
		       p.notification_template_id, nt.name AS notification_name,
		       nt.notification_type, p.resource_type, p.resource_id, p.event
		  FROM notification_policies p
		  JOIN notification_templates nt ON nt.id=p.notification_template_id
		  LEFT JOIN teams team ON team.id=p.team_id
		 WHERE p.resource_type=$1 AND p.resource_id=$2
		 ORDER BY p.event, team.name NULLS FIRST, nt.name, p.id`, resourceType, resourceID); err != nil {
		renderNotificationError(w, r, render.ErrInternal(err))
		return
	}
	render.JSON(w, r, policies)
}

// CreateNotificationPolicy POST /api/v1/notification-policies. Managing a
// route requires both administration of the automation resource and explicit
// notification administration for its organization.
func (h *NotificationsResource) CreateNotificationPolicy(w http.ResponseWriter, r *http.Request) {
	auditNotification(r, 0, 0, "notification_policy", "create", "")
	var body struct {
		NotificationTemplateID int64  `json:"notification_template_id"`
		ResourceType           string `json:"resource_type"`
		ResourceID             int64  `json:"resource_id"`
		TeamID                 *int64 `json:"team_id"`
		Event                  string `json:"event"`
	}
	if err := decodeStrictJSON(r, &body); err != nil || body.NotificationTemplateID <= 0 || body.ResourceID <= 0 || strings.TrimSpace(body.Event) == "" {
		renderNotificationError(w, r, render.ErrInvalidRequest(fmt.Errorf("notification_template_id, resource_type, resource_id, and event are required")))
		return
	}
	resource, err := h.resolvePolicyResource(r.Context(), body.ResourceType, body.ResourceID)
	if err != nil {
		renderNotificationError(w, r, render.ErrInvalidRequest(fmt.Errorf("unknown notification policy resource")))
		return
	}
	if !h.authorize(w, r, resource.ContentType, resource.ObjectID, actAdmin) ||
		!h.authorizeNotificationAdmin(w, r, resource.OrganizationID) {
		return
	}
	auditNotification(r, resource.OrganizationID, 0, "notification_policy", "create", "")

	var id int64
	err = h.DB.GetContext(r.Context(), &id, `
		INSERT INTO notification_policies (
			organization_id, team_id, notification_template_id,
			resource_type, resource_id, event
		) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT DO NOTHING
		RETURNING id`, resource.OrganizationID, body.TeamID, body.NotificationTemplateID, body.ResourceType, body.ResourceID, body.Event)
	if err == sql.ErrNoRows {
		err = h.DB.GetContext(r.Context(), &id, `
			SELECT id FROM notification_policies
			 WHERE organization_id=$1
			   AND team_id IS NOT DISTINCT FROM $2
			   AND notification_template_id=$3
			   AND resource_type=$4 AND resource_id=$5 AND event=$6`,
			resource.OrganizationID, body.TeamID, body.NotificationTemplateID, body.ResourceType, body.ResourceID, body.Event)
	}
	if err != nil {
		auditNotification(r, resource.OrganizationID, 0, "notification_policy", "create", "notification_scope_invalid")
		renderNotificationError(w, r, &render.ErrorResponse{
			Err:            err,
			HTTPStatusCode: http.StatusBadRequest,
			ErrorText:      "Invalid notification policy (notification_scope_invalid)",
		})
		return
	}
	auditNotification(r, resource.OrganizationID, id, "notification_policy", "create", "")
	render.Created(w, r, map[string]int64{"id": id})
}

// DeleteNotificationPolicy DELETE /api/v1/notification-policies/{id}.
func (h *NotificationsResource) DeleteNotificationPolicy(w http.ResponseWriter, r *http.Request) {
	auditNotification(r, 0, 0, "notification_policy", "delete", "")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		renderNotificationError(w, r, render.ErrInvalidRequest(fmt.Errorf("invalid notification policy id")))
		return
	}
	var policy struct {
		OrganizationID int64  `db:"organization_id"`
		ResourceType   string `db:"resource_type"`
		ResourceID     int64  `db:"resource_id"`
	}
	if err := h.DB.GetContext(r.Context(), &policy, `
			SELECT organization_id, resource_type, resource_id
			  FROM notification_policies WHERE id=$1`, id); err != nil {
		if err == sql.ErrNoRows {
			renderNotificationError(w, r, render.ErrNotFound(err))
			return
		}
		renderNotificationError(w, r, render.ErrInternal(err))
		return
	}
	resource, err := h.resolvePolicyResource(r.Context(), policy.ResourceType, policy.ResourceID)
	if err != nil {
		renderNotificationError(w, r, render.ErrInvalidRequest(fmt.Errorf("unknown notification policy resource")))
		return
	}
	if resource.OrganizationID != policy.OrganizationID {
		auditNotification(r, policy.OrganizationID, id, "notification_policy", "delete", "notification_scope_mismatch")
		renderNotificationError(w, r, render.ErrForbidden(fmt.Errorf("notification policy organization mismatch")))
		return
	}
	auditNotification(r, policy.OrganizationID, id, "notification_policy", "delete", "")
	if !h.authorize(w, r, resource.ContentType, resource.ObjectID, actAdmin) ||
		!h.authorizeNotificationAdmin(w, r, policy.OrganizationID) {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM notification_policies WHERE id=$1`, id); err != nil {
		renderNotificationError(w, r, render.ErrInternal(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestNotificationTemplate POST /api/v1/notification-templates/{id}/test
// sends a bounded synthetic message through the same decrypt-and-deliver path
// used by lifecycle notifications. Stored config remains server-side and is
// never serialized into the response.
func (h *NotificationsResource) TestNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	auditNotification(r, 0, 0, "notification_template", "test", "")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		renderNotificationError(w, r, render.ErrInvalidRequest(fmt.Errorf("invalid notification template id")))
		return
	}
	var target notificationDeliveryTarget
	if err := h.DB.GetContext(r.Context(), &target, `
		SELECT id, organization_id, name, notification_type, config
		FROM notification_templates WHERE id = $1`, id); err != nil {
		if err == sql.ErrNoRows {
			renderNotificationError(w, r, render.ErrNotFound(err))
			return
		}
		renderNotificationError(w, r, render.ErrInternal(err))
		return
	}
	auditNotification(r, target.OrganizationID, target.ID, "notification_template", "test", "")
	if !h.authorizeNotificationAdmin(w, r, target.OrganizationID) {
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
		auditNotification(r, target.OrganizationID, target.ID, "notification_template", "test", failureCode)
		// Delivery errors can contain a webhook URL whose path embeds a secret.
		// Record only bounded identifiers and a stable failure code.
		logger.Warn("notification test delivery failed", "notification_template_id", target.ID, "organization_id", target.OrganizationID, "notification_type", target.NotificationType, "failure_code", failureCode)
		renderNotificationError(w, r, &render.ErrorResponse{Err: err, HTTPStatusCode: http.StatusBadGateway, ErrorText: "Test notification could not be delivered (" + failureCode + ")"})
		return
	}
	render.JSON(w, r, map[string]interface{}{
		"status":                   "delivered",
		"notification_template_id": target.ID,
		"tested_at":                time.Now().UTC(),
	})
}

type notificationErrorRenderer interface {
	Render(http.ResponseWriter, *http.Request) error
}

func renderNotificationError(w http.ResponseWriter, r *http.Request, response notificationErrorRenderer) {
	if err := response.Render(w, r); err != nil {
		logger.Error("notification error response rendering failed", "error", err)
	}
}

// DeleteNotificationTemplate DELETE /api/v1/notification-templates/{id}
func (h *NotificationsResource) DeleteNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	auditNotification(r, 0, 0, "notification_template", "delete", "")
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
	auditNotification(r, orgID, id, "notification_template", "delete", "")
	if !h.authorizeNotificationAdmin(w, r, orgID) {
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
	auditNotificationAttachment(rs.DB, r, "job_template", jtID, "create")
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
	auditNotificationAttachment(rs.DB, r, "job_template", jtID, "delete")
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
	auditNotificationAttachment(rs.DB, r, "workflow_template", wtID, "create")
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
	auditNotificationAttachment(rs.DB, r, "workflow_template", wtID, "delete")
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
