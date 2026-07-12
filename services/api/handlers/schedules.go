package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
	"github.com/praetordev/rbac"
	"github.com/praetordev/praetor/services/api/store"
	"github.com/teambition/rrule-go"
)

// ScheduleStore is the schedules-domain data access the handler depends on.
type ScheduleStore interface {
	ListAll(ctx context.Context) ([]models.Schedule, error)
	ListByTargetOrgIDs(ctx context.Context, orgIDs []int64) ([]models.Schedule, error)
	Get(ctx context.Context, id int64) (models.Schedule, error)
	Create(ctx context.Context, sched models.Schedule) (int64, error)
	Update(ctx context.Context, sched models.Schedule) error
	Delete(ctx context.Context, id int64) error
	TargetOrg(ctx context.Context, wfID, ujtID *int64) (int64, bool)
	ScheduleOrg(ctx context.Context, id int64) (int64, bool)
}

type SchedulesResource struct {
	DB *sqlx.DB
	*Authorizer
	store ScheduleStore
}

func NewSchedulesResource(db *sqlx.DB) *SchedulesResource {
	return &SchedulesResource{DB: db, Authorizer: NewAuthorizer(db), store: store.NewScheduleStore(db)}
}

func (rs *SchedulesResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListSchedules)
	r.Post("/", rs.CreateSchedule)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", rs.GetSchedule)
		r.Put("/", rs.UpdateSchedule)
		r.Delete("/", rs.DeleteSchedule)
	})
	return r
}

func (rs *SchedulesResource) ListSchedules(w http.ResponseWriter, r *http.Request) {
	uc := currentUser(r)
	var schedules []models.Schedule
	var err error
	if uc.IsSuperuser || uc.IsSystemAuditor {
		if schedules, err = rs.store.ListAll(r.Context()); err != nil {
			render.Render(w, r, ErrInternal(err))
			return
		}
		render.JSON(w, r, schedules)
		return
	}
	// Scope to schedules whose target lives in an organization the user can read.
	ids, err := rs.readableIDs(r, rbac.ContentTypeOrganization)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	if schedules, err = rs.store.ListByTargetOrgIDs(r.Context(), ids); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, schedules)
}

func (rs *SchedulesResource) GetSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if org, ok := rs.store.ScheduleOrg(r.Context(), id); !ok {
		render.Render(w, r, ErrNotFound)
		return
	} else if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actRead) {
		return
	}
	sched, err := rs.store.Get(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		render.Render(w, r, ErrNotFound)
		return
	} else if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, sched)
}

func (rs *SchedulesResource) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var sched models.Schedule
	if err := json.NewDecoder(r.Body).Decode(&sched); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Validation: name + rrule, and exactly one target (job template XOR workflow).
	if sched.Name == "" || sched.RRule == "" {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}
	if (sched.UnifiedJobTemplateID == nil) == (sched.WorkflowTemplateID == nil) {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}
	// Only an admin of the target's organization may schedule it.
	org, ok := rs.store.TargetOrg(r.Context(), sched.WorkflowTemplateID, sched.UnifiedJobTemplateID)
	if !ok {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}

	// Validate RRULE
	rule, err := rrule.StrToRRule(sched.RRule)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Calculate NextRun
	sched.NextRun = rule.After(time.Now(), false)
	sched.CreatedAt = time.Now()
	sched.ModifiedAt = time.Now()
	sched.Enabled = true // Default enabled
	if len(sched.ExtraVars) == 0 {
		sched.ExtraVars = json.RawMessage("{}") // jsonb column rejects an empty value
	}

	newID, err := rs.store.Create(r.Context(), sched)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	sched.ID = newID

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, sched)
}

func (rs *SchedulesResource) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Must admin the schedule's current org.
	curOrg, ok := rs.store.ScheduleOrg(r.Context(), id)
	if !ok {
		render.Render(w, r, ErrNotFound)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, curOrg, actAdmin) {
		return
	}

	var sched models.Schedule
	if err := json.NewDecoder(r.Body).Decode(&sched); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}
	sched.ID = id
	// If the target changed, the caller must also admin the new target's org.
	if newOrg, ok := rs.store.TargetOrg(r.Context(), sched.WorkflowTemplateID, sched.UnifiedJobTemplateID); ok && newOrg != curOrg {
		if !rs.authorize(w, r, rbac.ContentTypeOrganization, newOrg, actAdmin) {
			return
		}
	}

	// Validate RRULE if changed (simplified: always validate)
	rule, err := rrule.StrToRRule(sched.RRule)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Recalculate NextRun
	sched.NextRun = rule.After(time.Now(), false)
	sched.ModifiedAt = time.Now()
	if len(sched.ExtraVars) == 0 {
		sched.ExtraVars = json.RawMessage("{}")
	}

	if err := rs.store.Update(r.Context(), sched); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	render.JSON(w, r, sched)
}

func (rs *SchedulesResource) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	org, ok := rs.store.ScheduleOrg(r.Context(), id)
	if !ok {
		render.Render(w, r, ErrNotFound)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}
	if err := rs.store.Delete(r.Context(), id); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -- Err Helpers --
// Note: ErrInternal, ErrInvalidRequest, etc. are defined in jobs.go which is in the same package.
