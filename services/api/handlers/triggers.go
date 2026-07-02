package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

// TriggersResource manages event triggers (launch a target when a job reaches a
// terminal state) and exposes a read-only view of inbound webhook triggers. It
// sits alongside Schedules as the third way to launch workflows/templates.
type TriggersResource struct {
	DB *sqlx.DB
	*Authorizer
}

func NewTriggersResource(db *sqlx.DB) *TriggersResource {
	return &TriggersResource{DB: db, Authorizer: NewAuthorizer(db)}
}

const eventTriggerCols = `id, organization_id, name, enabled, event_type, source_ujt_id, workflow_template_id, unified_job_template_id, created_at`

// triggerOrg returns the organization that owns an event trigger.
func (rs *TriggersResource) triggerOrg(r *http.Request, id int64) (int64, bool) {
	var org int64
	err := rs.DB.GetContext(r.Context(), &org, `SELECT organization_id FROM event_triggers WHERE id=$1`, id)
	return org, err == nil
}

func (rs *TriggersResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/event", rs.ListEvent)
	r.Post("/event", rs.CreateEvent)
	r.Put("/event/{id}", rs.UpdateEvent)
	r.Delete("/event/{id}", rs.DeleteEvent)
	r.Get("/webhook", rs.ListWebhook)
	return r
}

type eventTrigger struct {
	ID                   int64     `json:"id" db:"id"`
	OrganizationID       int64     `json:"organization_id" db:"organization_id"`
	Name                 string    `json:"name" db:"name"`
	Enabled              bool      `json:"enabled" db:"enabled"`
	EventType            string    `json:"event_type" db:"event_type"`
	SourceUJTID          *int64    `json:"source_ujt_id,omitempty" db:"source_ujt_id"`
	WorkflowTemplateID   *int64    `json:"workflow_template_id,omitempty" db:"workflow_template_id"`
	UnifiedJobTemplateID *int64    `json:"unified_job_template_id,omitempty" db:"unified_job_template_id"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
}

var validEventTypes = map[string]bool{"job_succeeded": true, "job_failed": true, "job_finished": true}

func (rs *TriggersResource) ListEvent(w http.ResponseWriter, r *http.Request) {
	rows := []eventTrigger{}
	uc := currentUser(r)
	if uc.IsSuperuser || uc.IsSystemAuditor {
		if err := rs.DB.SelectContext(r.Context(), &rows,
			`SELECT `+eventTriggerCols+` FROM event_triggers ORDER BY id`); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.JSON(w, r, rows)
		return
	}
	ids, err := rs.readableIDs(r, rbac.ContentTypeOrganization)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if len(ids) > 0 {
		q, args, _ := sqlx.In(`SELECT `+eventTriggerCols+` FROM event_triggers WHERE organization_id IN (?) ORDER BY id`, ids)
		q = rs.DB.Rebind(q)
		if err := rs.DB.SelectContext(r.Context(), &rows, q, args...); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
	}
	render.JSON(w, r, rows)
}

func (rs *TriggersResource) CreateEvent(w http.ResponseWriter, r *http.Request) {
	var in eventTrigger
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || !validEventTypes[in.EventType] {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	// Exactly one target: a workflow or a job template.
	if (in.WorkflowTemplateID == nil) == (in.UnifiedJobTemplateID == nil) {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	// Only an admin of the trigger's organization may create it.
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, in.OrganizationID, actAdmin) {
		return
	}
	// Enabled by default (an absent JSON bool is false, which we don't want here).
	var created eventTrigger
	if err := rs.DB.QueryRowxContext(r.Context(),
		`INSERT INTO event_triggers (organization_id, name, enabled, event_type, source_ujt_id, workflow_template_id, unified_job_template_id)
		 VALUES ($1,$2,true,$3,$4,$5,$6)
		 RETURNING id, organization_id, name, enabled, event_type, source_ujt_id, workflow_template_id, unified_job_template_id, created_at`,
		in.OrganizationID, in.Name, in.EventType, in.SourceUJTID, in.WorkflowTemplateID, in.UnifiedJobTemplateID,
	).StructScan(&created); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, created)
}

// UpdateEvent PUT /triggers/event/{id} — edit a trigger or toggle enabled. Unlike
// create, enabled is taken verbatim (the client always sends the current value).
func (rs *TriggersResource) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	// Gate on the trigger's current organization.
	org, ok := rs.triggerOrg(r, id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}
	var in eventTrigger
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || !validEventTypes[in.EventType] {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if (in.WorkflowTemplateID == nil) == (in.UnifiedJobTemplateID == nil) {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	var updated eventTrigger
	if err := rs.DB.QueryRowxContext(r.Context(),
		`UPDATE event_triggers SET name=$2, enabled=$3, event_type=$4, source_ujt_id=$5, workflow_template_id=$6, unified_job_template_id=$7
		 WHERE id=$1
		 RETURNING id, organization_id, name, enabled, event_type, source_ujt_id, workflow_template_id, unified_job_template_id, created_at`,
		id, in.Name, in.Enabled, in.EventType, in.SourceUJTID, in.WorkflowTemplateID, in.UnifiedJobTemplateID,
	).StructScan(&updated); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, updated)
}

func (rs *TriggersResource) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	org, ok := rs.triggerOrg(r, id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}
	if _, err := rs.DB.ExecContext(r.Context(), `DELETE FROM event_triggers WHERE id=$1`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type webhookTrigger struct {
	Kind    string `json:"kind"` // workflow | job_template
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Service string `json:"service"`
	URL     string `json:"url"`
}

// ListWebhook surfaces every workflow/template that has an inbound webhook trigger
// enabled, with the URL to POST to — the secret is never returned.
func (rs *TriggersResource) ListWebhook(w http.ResponseWriter, r *http.Request) {
	out := []webhookTrigger{}
	type row struct {
		ID      int64  `db:"id"`
		Name    string `db:"name"`
		Service string `db:"service"`
	}
	uc := currentUser(r)
	all := uc.IsSuperuser || uc.IsSystemAuditor
	var orgIDs []int64
	if !all {
		var err error
		if orgIDs, err = rs.readableIDs(r, rbac.ContentTypeOrganization); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if len(orgIDs) == 0 {
			render.JSON(w, r, out) // nothing readable
			return
		}
	}

	// selectScoped runs a webhook query, org-filtered for non-privileged users.
	selectScoped := func(dst *[]row, base string) {
		if all {
			_ = rs.DB.SelectContext(r.Context(), dst, base+" ORDER BY name")
			return
		}
		q, args, _ := sqlx.In(base+" AND organization_id IN (?) ORDER BY name", orgIDs)
		q = rs.DB.Rebind(q)
		_ = rs.DB.SelectContext(r.Context(), dst, q, args...)
	}

	wf := []row{}
	selectScoped(&wf, `SELECT id, name, COALESCE(webhook_service,'generic') AS service FROM workflow_templates WHERE webhook_enabled`)
	for _, x := range wf {
		out = append(out, webhookTrigger{Kind: "workflow", ID: x.ID, Name: x.Name, Service: x.Service,
			URL: fmt.Sprintf("/api/v1/webhooks/workflow-templates/%d/%s", x.ID, x.Service)})
	}
	jt := []row{}
	selectScoped(&jt, `SELECT id, name, COALESCE(webhook_service,'generic') AS service FROM job_templates WHERE webhook_enabled`)
	for _, x := range jt {
		out = append(out, webhookTrigger{Kind: "job_template", ID: x.ID, Name: x.Name, Service: x.Service,
			URL: fmt.Sprintf("/api/v1/webhooks/job-templates/%d/%s", x.ID, x.Service)})
	}
	// Execution packs are shared infrastructure managed by superusers, so only they
	// see pack build triggers. Packs have no service; the URL takes it as a param.
	if uc.IsSuperuser {
		ep := []row{}
		_ = rs.DB.SelectContext(r.Context(), &ep,
			`SELECT id, name, 'generic' AS service FROM execution_packs WHERE webhook_key IS NOT NULL AND webhook_key <> '' ORDER BY name`)
		for _, x := range ep {
			out = append(out, webhookTrigger{Kind: "execution_pack", ID: x.ID, Name: x.Name, Service: x.Service,
				URL: fmt.Sprintf("/api/v1/webhooks/execution-packs/%d/%s", x.ID, x.Service)})
		}
	}
	render.JSON(w, r, out)
}
