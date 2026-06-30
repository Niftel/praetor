package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

// TemplatesResource handles job template operations
type TemplatesResource struct {
	DB *sqlx.DB
	*Authorizer
}

// NewTemplatesResource creates a new templates resource handler
func NewTemplatesResource(db *sqlx.DB) *TemplatesResource {
	return &TemplatesResource{DB: db, Authorizer: NewAuthorizer(db)}
}

// Routes creates a REST router for the Templates resource
func (rs *TemplatesResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListTemplates)
	r.Post("/", rs.CreateTemplate)
	r.Get("/{id}", rs.GetTemplate)
	r.Put("/{id}", rs.UpdateTemplate)
	r.Delete("/{id}", rs.DeleteTemplate)
	return r
}

// ListTemplates GET /api/v1/job-templates
func (rs *TemplatesResource) ListTemplates(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)
	uc := currentUser(r)

	var templates []models.JobTemplate
	var total int64

	if uc.IsSuperuser || uc.IsSystemAuditor {
		if err := rs.DB.SelectContext(r.Context(), &templates, `SELECT * FROM job_templates ORDER BY id DESC LIMIT $1 OFFSET $2`, pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		_ = rs.DB.Get(&total, "SELECT count(*) FROM job_templates")
	} else {
		ids, err := rs.readableIDs(r, rbac.ContentTypeJobTemplate)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if len(ids) > 0 {
			q, args, _ := sqlx.In(`SELECT * FROM job_templates WHERE id IN (?) ORDER BY id DESC LIMIT ? OFFSET ?`, ids, pg.Limit, pg.Offset)
			q = rs.DB.Rebind(q)
			if err := rs.DB.SelectContext(r.Context(), &templates, q, args...); err != nil {
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

	// Default organization to 1 if not set
	if input.OrganizationID == 0 {
		input.OrganizationID = 1
	}

	// Validation: Playbook is required if no content provided
	if input.Playbook == "" && input.PlaybookContent == nil {
		render.ErrInvalidRequest(nil).Render(w, r)
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

	// Creating a template requires admin on its org, plus use access on any
	// project/inventory/credential it attaches (AWX attach semantics).
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, input.OrganizationID, actAdmin) {
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

	tx, err := rs.DB.Beginx()
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer tx.Rollback()

	// 1. Insert into unified_job_templates to get ID
	var ujtID int64
	err = tx.QueryRowxContext(r.Context(), "INSERT INTO unified_job_templates (name) VALUES ($1) RETURNING id", input.Name).Scan(&ujtID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	input.UnifiedJobTemplateID = &ujtID

	// 2. Insert into job_templates
	query := `
		INSERT INTO job_templates (organization_id, name, description, playbook, playbook_content, project_id, inventory_id, job_type, verbosity, unified_job_template_id, credential_id, extra_vars, job_limit, ask_variables_on_launch, ask_limit_on_launch, survey_enabled, survey_spec)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING *`

	var created models.JobTemplate
	err = tx.QueryRowxContext(r.Context(), query,
		input.OrganizationID, input.Name, input.Description,
		input.Playbook, input.PlaybookContent, input.ProjectID, input.InventoryID,
		input.JobType, input.Verbosity, ujtID, input.CredentialID,
		input.ExtraVars, input.JobLimit, input.AskVariablesOnLaunch, input.AskLimitOnLaunch,
		input.SurveyEnabled, input.SurveySpec,
	).StructScan(&created)

	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if err := tx.Commit(); err != nil {
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

	var template models.JobTemplate
	query := `SELECT * FROM job_templates WHERE id = $1`
	err = rs.DB.GetContext(r.Context(), &template, query, id)
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

	if input.ExtraVars == nil {
		input.ExtraVars = json.RawMessage("{}")
	}
	if input.SurveySpec == nil {
		input.SurveySpec = json.RawMessage("{}")
	}

	query := `
		UPDATE job_templates
		SET name = $2, description = $3, playbook = $4, playbook_content = $5,
		    project_id = $6, verbosity = $7, inventory_id = $8, credential_id = $9,
		    extra_vars = $10, job_limit = $11, ask_variables_on_launch = $12, ask_limit_on_launch = $13,
		    survey_enabled = $14, survey_spec = $15,
		    modified_at = now()
		WHERE id = $1
		RETURNING *`

	var updated models.JobTemplate
	err = rs.DB.QueryRowxContext(r.Context(), query,
		id, input.Name, input.Description, input.Playbook,
		input.PlaybookContent, input.ProjectID, input.Verbosity, input.InventoryID, input.CredentialID,
		input.ExtraVars, input.JobLimit, input.AskVariablesOnLaunch, input.AskLimitOnLaunch,
		input.SurveyEnabled, input.SurveySpec,
	).StructScan(&updated)

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

	query := `DELETE FROM job_templates WHERE id = $1`
	_, err = rs.DB.ExecContext(r.Context(), query, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
