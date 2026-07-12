package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
	"github.com/praetordev/rbac"
	"github.com/praetordev/render"
	"github.com/praetordev/praetor/services/api/store"
)

// requireSCMPlaybook enforces that a job template's playbook comes from source
// control: inline playbook content is disabled, and the template must reference a
// project (SCM) plus a playbook path within it. It also clears any inline content
// so it is never stored. Applied on both create and update.
func requireSCMPlaybook(input *models.JobTemplate) error {
	if input.PlaybookContent != nil && strings.TrimSpace(*input.PlaybookContent) != "" {
		return fmt.Errorf("inline playbooks are disabled; commit the playbook to a source-control project and reference it")
	}
	input.PlaybookContent = nil // never persist inline content
	if input.ProjectID == nil {
		return fmt.Errorf("a project (source control) is required")
	}
	if strings.TrimSpace(input.Playbook) == "" {
		return fmt.Errorf("a playbook path within the project is required")
	}
	return nil
}

// genWebhookKey returns a random shared secret for verifying inbound webhooks.
func genWebhookKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// TemplateStore is the templates-domain data access the handler depends on
// (implemented by services/api/store.TemplateStore). Declared consumer-side.
type TemplateStore interface {
	ListAll(ctx context.Context, limit, offset int) ([]models.JobTemplate, error)
	CountAll(ctx context.Context) (int64, error)
	ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.JobTemplate, error)
	Get(ctx context.Context, id int64) (models.JobTemplate, error)
	Create(ctx context.Context, input models.JobTemplate) (models.JobTemplate, error)
	Update(ctx context.Context, id int64, input models.JobTemplate) (models.JobTemplate, error)
	Delete(ctx context.Context, id int64) error
}

// TemplatesResource handles job template operations
type TemplatesResource struct {
	DB *sqlx.DB
	*Authorizer
	store         TemplateStore
	notifications NotificationStore
}

// NewTemplatesResource creates a new templates resource handler
func NewTemplatesResource(db *sqlx.DB, authz *Authorizer) *TemplatesResource {
	return &TemplatesResource{DB: db, Authorizer: authz, store: store.NewTemplateStore(db), notifications: store.NewNotificationStore(db)}
}

// Routes creates a REST router for the Templates resource
func (rs *TemplatesResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListTemplates)
	r.Post("/", rs.CreateTemplate)
	r.Get("/{id}", rs.GetTemplate)
	r.Put("/{id}", rs.UpdateTemplate)
	r.Delete("/{id}", rs.DeleteTemplate)
	// Notification attachments (which notification fires on which event).
	r.Get("/{id}/notifications", rs.ListJobTemplateNotifications)
	r.Post("/{id}/notifications", rs.AttachJobTemplateNotification)
	r.Delete("/{id}/notifications/{ntId}/{event}", rs.DetachJobTemplateNotification)
	return r
}

// ListTemplates GET /api/v1/job-templates
func (rs *TemplatesResource) ListTemplates(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)
	var templates []models.JobTemplate
	var total int64

	viewAll, verr := rs.canViewAll(r, rbac.ContentTypeJobTemplate)
	if verr != nil {
		render.ErrInternal(verr).Render(w, r)
		return
	}
	if viewAll {
		var err error
		if templates, err = rs.store.ListAll(r.Context(), pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total, _ = rs.store.CountAll(r.Context())
	} else {
		ids, err := rs.readableIDs(r, rbac.ContentTypeJobTemplate)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if len(ids) > 0 {
			if templates, err = rs.store.ListByIDs(r.Context(), ids, pg.Limit, pg.Offset); err != nil {
				render.ErrInternal(err).Render(w, r)
				return
			}
			total = int64(len(ids))
		}
	}

	if templates == nil {
		templates = []models.JobTemplate{}
	}

	render.JSON(w, r, &render.PaginatedResponse{
		Items:  templates,
		Total:  total,
		Limit:  pg.Limit,
		Offset: pg.Offset,
	})
}

// CreateTemplate POST /api/v1/job-templates
func (rs *TemplatesResource) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var input models.JobTemplate
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Validation
	if input.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	// A job template must belong to an explicit organization (no silent org-1 default).
	if input.OrganizationID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r) // organization_id is required
		return
	}

	// Playbooks must come from source control — inline content is disabled.
	if err := requireSCMPlaybook(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Default job type
	if input.JobType == "" {
		input.JobType = "run"
	}
	// extra_vars / survey_spec are jsonb; default absent values to empty objects.
	if input.ExtraVars == nil {
		input.ExtraVars = json.RawMessage("{}")
	}
	if input.SurveySpec == nil {
		input.SurveySpec = json.RawMessage("{}")
	}
	if input.WebhookEnabled && input.WebhookKey == "" {
		input.WebhookKey = genWebhookKey()
	}

	// Creating a template requires the org's job_template_admin_role, plus use
	// access on any project/inventory/credential it attaches (AWX attach
	// semantics). Org admins/superusers inherit job_template_admin_role.
	if !rs.authorizeOrgRole(w, r, input.OrganizationID, rbac.RoleFieldJobTemplateAdmin) {
		return
	}
	if input.ProjectID != nil && !rs.authorize(w, r, rbac.ContentTypeProject, *input.ProjectID, actUse) {
		return
	}
	if input.InventoryID != nil && !rs.authorize(w, r, rbac.ContentTypeInventory, *input.InventoryID, actUse) {
		return
	}
	if input.CredentialID != nil && !rs.authorize(w, r, rbac.ContentTypeCredential, *input.CredentialID, actUse) {
		return
	}

	created, err := rs.store.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	rs.grantCreatorAdmin(r.Context(), rbac.ContentTypeJobTemplate, created.ID, currentUser(r))
	render.Created(w, r, created)
}

// GetTemplate GET /api/v1/job-templates/{id}
func (rs *TemplatesResource) GetTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, id, actRead) {
		return
	}

	template, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	render.JSON(w, r, template)
}

// UpdateTemplate PUT /api/v1/job-templates/{id}
func (rs *TemplatesResource) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, id, actAdmin) {
		return
	}

	var input models.JobTemplate
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Playbooks must come from source control — inline content is disabled.
	if err := requireSCMPlaybook(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if input.ExtraVars == nil {
		input.ExtraVars = json.RawMessage("{}")
	}
	if input.SurveySpec == nil {
		input.SurveySpec = json.RawMessage("{}")
	}
	if input.WebhookEnabled && input.WebhookKey == "" {
		input.WebhookKey = genWebhookKey()
	}

	// Re-check use-on-reference for any project/inventory/credential the update
	// ATTACHES or CHANGES — admin on the template alone must not let a user point
	// it at a resource they lack `use` on (AWX validates use on changed relations).
	existing, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.Render(w, r, render.ErrNotFound(nil))
		return
	}
	changed := func(newV, oldV *int64) bool {
		return newV != nil && (oldV == nil || *newV != *oldV)
	}
	if changed(input.ProjectID, existing.ProjectID) && !rs.authorize(w, r, rbac.ContentTypeProject, *input.ProjectID, actUse) {
		return
	}
	if changed(input.InventoryID, existing.InventoryID) && !rs.authorize(w, r, rbac.ContentTypeInventory, *input.InventoryID, actUse) {
		return
	}
	if changed(input.CredentialID, existing.CredentialID) && !rs.authorize(w, r, rbac.ContentTypeCredential, *input.CredentialID, actUse) {
		return
	}

	updated, err := rs.store.Update(r.Context(), id, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, updated)
}

// DeleteTemplate DELETE /api/v1/job-templates/{id}
func (rs *TemplatesResource) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, id, actAdmin) {
		return
	}

	if err := rs.store.Delete(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
