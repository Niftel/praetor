package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
)

// TriggerStore is the triggers-domain data access the handler depends on.
type TriggerStore interface {
	TriggerOrg(ctx context.Context, id int64) (int64, bool)
	ListEventAll(ctx context.Context) ([]store.EventTrigger, error)
	ListEventByOrgs(ctx context.Context, orgIDs []int64) ([]store.EventTrigger, error)
	CreateEvent(ctx context.Context, in store.EventTrigger) (store.EventTrigger, error)
	UpdateEvent(ctx context.Context, id int64, in store.EventTrigger) (store.EventTrigger, error)
	DeleteEvent(ctx context.Context, id int64) error
	WebhookWorkflows(ctx context.Context, all bool, orgIDs []int64) ([]store.WebhookSourceRow, error)
	WebhookJobTemplates(ctx context.Context, all bool, orgIDs []int64) ([]store.WebhookSourceRow, error)
	WebhookPacks(ctx context.Context) ([]store.WebhookSourceRow, error)
}

// TriggersResource manages event triggers (launch a target when a job reaches a
// terminal state) and exposes a read-only view of inbound webhook triggers. It
// sits alongside Schedules as the third way to launch workflows/templates.
type TriggersResource struct {
	DB *sqlx.DB
	*Authorizer
	store TriggerStore
}

func NewTriggersResource(db *sqlx.DB, authz *Authorizer) *TriggersResource {
	return &TriggersResource{DB: db, Authorizer: authz, store: store.NewTriggerStore(db)}
}

// eventTrigger aliases the store DTO so handler code reads unchanged.
type eventTrigger = store.EventTrigger

func (rs *TriggersResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/event", rs.ListEvent)
	r.Post("/event", rs.CreateEvent)
	r.Put("/event/{id}", rs.UpdateEvent)
	r.Delete("/event/{id}", rs.DeleteEvent)
	r.Get("/webhook", rs.ListWebhook)
	return r
}

var validEventTypes = map[string]bool{"job_succeeded": true, "job_failed": true, "job_finished": true}

func (rs *TriggersResource) ListEvent(w http.ResponseWriter, r *http.Request) {
	viewAll, verr := rs.canViewAll(r, rbac.Organization)
	if verr != nil {
		render.ErrInternal(verr).Render(w, r)
		return
	}
	if viewAll {
		rows, err := rs.store.ListEventAll(r.Context())
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.JSON(w, r, rows)
		return
	}
	ids, err := rs.readableIDs(r, rbac.Organization)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	rows, err := rs.store.ListEventByOrgs(r.Context(), ids)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
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
	if !rs.authorize(w, r, rbac.Organization, in.OrganizationID, actAdmin) {
		return
	}
	// Enabled by default (an absent JSON bool is false, which we don't want here).
	created, err := rs.store.CreateEvent(r.Context(), in)
	if err != nil {
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
	org, ok := rs.store.TriggerOrg(r.Context(), id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.Organization, org, actAdmin) {
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
	updated, err := rs.store.UpdateEvent(r.Context(), id, in)
	if err != nil {
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
	org, ok := rs.store.TriggerOrg(r.Context(), id)
	if !ok {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.Organization, org, actAdmin) {
		return
	}
	if err := rs.store.DeleteEvent(r.Context(), id); err != nil {
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
	all, verr := rs.canViewAll(r, rbac.Organization)
	if verr != nil {
		render.ErrInternal(verr).Render(w, r)
		return
	}
	var orgIDs []int64
	if !all {
		var err error
		if orgIDs, err = rs.readableIDs(r, rbac.Organization); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if len(orgIDs) == 0 {
			render.JSON(w, r, out) // nothing readable
			return
		}
	}

	wf, _ := rs.store.WebhookWorkflows(r.Context(), all, orgIDs)
	for _, x := range wf {
		out = append(out, webhookTrigger{Kind: "workflow", ID: x.ID, Name: x.Name, Service: x.Service,
			URL: fmt.Sprintf("/api/v1/webhooks/workflow-templates/%d/%s", x.ID, x.Service)})
	}
	jt, _ := rs.store.WebhookJobTemplates(r.Context(), all, orgIDs)
	for _, x := range jt {
		out = append(out, webhookTrigger{Kind: "job_template", ID: x.ID, Name: x.Name, Service: x.Service,
			URL: fmt.Sprintf("/api/v1/webhooks/job-templates/%d/%s", x.ID, x.Service)})
	}
	// Execution packs are shared infrastructure; only holders of the global
	// manage_executionpack capability see pack build triggers. Packs have no
	// service; the URL takes it as a param.
	packAdmin, err := rs.holdsGlobal(r, rbac.ManageExecutionPacks)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if packAdmin {
		ep, _ := rs.store.WebhookPacks(r.Context())
		for _, x := range ep {
			out = append(out, webhookTrigger{Kind: "execution_pack", ID: x.ID, Name: x.Name, Service: x.Service,
				URL: fmt.Sprintf("/api/v1/webhooks/execution-packs/%d/%s", x.ID, x.Service)})
		}
	}
	render.JSON(w, r, out)
}
