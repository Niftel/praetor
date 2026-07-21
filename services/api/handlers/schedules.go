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
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/store"
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
	TargetOrg(ctx context.Context, wfID, ujtID, sourceID *int64) (int64, bool)
	ScheduleOrg(ctx context.Context, id int64) (int64, bool)
	InventoryIDBySource(ctx context.Context, sourceID int64) (int64, bool)
}

type SchedulesResource struct {
	DB *sqlx.DB
	*Authorizer
	store ScheduleStore
}

func NewSchedulesResource(db *sqlx.DB, authz *Authorizer) *SchedulesResource {
	return &SchedulesResource{DB: db, Authorizer: authz, store: store.NewScheduleStore(db)}
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
	var schedules []models.Schedule
	var err error
	viewAll, verr := rs.canViewAll(r, rbac.Organization)
	if verr != nil {
		render.Render(w, r, ErrInternal(verr))
		return
	}
	if viewAll {
		if schedules, err = rs.store.ListAll(r.Context()); err != nil {
			render.Render(w, r, ErrInternal(err))
			return
		}
		render.JSON(w, r, dto.FromSchedules(schedules))
		return
	}
	// Scope to schedules whose target lives in an organization the user can read.
	ids, err := rs.readableIDs(r, rbac.Organization)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	if schedules, err = rs.store.ListByTargetOrgIDs(r.Context(), ids); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	visible := schedules[:0]
	for _, sched := range schedules {
		if sched.InventorySourceID == nil {
			visible = append(visible, sched)
			continue
		}
		inventoryID, ok := rs.store.InventoryIDBySource(r.Context(), *sched.InventorySourceID)
		if !ok {
			continue
		}
		allowed, aerr := rs.canAuthorize(r, rbac.Inventory, inventoryID, actRead)
		if aerr != nil {
			render.Render(w, r, ErrInternal(aerr))
			return
		}
		if allowed {
			visible = append(visible, sched)
		}
	}
	render.JSON(w, r, dto.FromSchedules(visible))
}

func (rs *SchedulesResource) GetSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	sched, err := rs.store.Get(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		render.Render(w, r, ErrNotFound)
		return
	} else if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	if sched.InventorySourceID != nil {
		inventoryID, ok := rs.store.InventoryIDBySource(r.Context(), *sched.InventorySourceID)
		if !ok {
			render.Render(w, r, ErrNotFound)
			return
		}
		if !rs.authorize(w, r, rbac.Inventory, inventoryID, actRead) {
			return
		}
	} else if org, ok := rs.store.ScheduleOrg(r.Context(), id); !ok {
		render.Render(w, r, ErrNotFound)
		return
	} else if !rs.authorize(w, r, rbac.Organization, org, actRead) {
		return
	}
	render.JSON(w, r, dto.FromSchedule(sched))
}

func (rs *SchedulesResource) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var body dto.Schedule
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}
	sched := body.ToModel()

	// Validation: name + rrule, and exactly one target.
	if sched.Name == "" || sched.RRule == "" {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}
	targets := 0
	for _, target := range []*int64{sched.UnifiedJobTemplateID, sched.WorkflowTemplateID, sched.InventorySourceID} {
		if target != nil {
			targets++
		}
	}
	if targets != 1 {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}
	// Only an admin of the target's organization may schedule it.
	org, ok := rs.store.TargetOrg(r.Context(), sched.WorkflowTemplateID, sched.UnifiedJobTemplateID, sched.InventorySourceID)
	if !ok {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}
	if sched.InventorySourceID != nil {
		inventoryID, found := rs.store.InventoryIDBySource(r.Context(), *sched.InventorySourceID)
		if !found || !rs.authorize(w, r, rbac.Inventory, inventoryID, actUpdate) {
			return
		}
	} else if !rs.authorize(w, r, rbac.Organization, org, actAdmin) {
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
	actorID := currentUser(r).UserID
	sched.ActorUserID = &actorID
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
	render.JSON(w, r, dto.FromSchedule(sched))
}

func (rs *SchedulesResource) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Must admin the schedule's current org.
	existing, getErr := rs.store.Get(r.Context(), id)
	if getErr != nil {
		render.Render(w, r, ErrNotFound)
		return
	}
	curOrg, ok := rs.store.ScheduleOrg(r.Context(), id)
	if !ok {
		render.Render(w, r, ErrNotFound)
		return
	}
	if existing.InventorySourceID != nil {
		inventoryID, found := rs.store.InventoryIDBySource(r.Context(), *existing.InventorySourceID)
		if !found || !rs.authorize(w, r, rbac.Inventory, inventoryID, actUpdate) {
			return
		}
	} else if !rs.authorize(w, r, rbac.Organization, curOrg, actAdmin) {
		return
	}

	var body dto.Schedule
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}
	sched := body.ToModel()
	sched.ID = id
	targets := 0
	for _, target := range []*int64{sched.UnifiedJobTemplateID, sched.WorkflowTemplateID, sched.InventorySourceID} {
		if target != nil {
			targets++
		}
	}
	if targets != 1 {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}
	// The caller must hold the target-specific scheduling permission even when
	// switching between targets in the same organization.
	newOrg, targetExists := rs.store.TargetOrg(r.Context(), sched.WorkflowTemplateID, sched.UnifiedJobTemplateID, sched.InventorySourceID)
	if !targetExists {
		render.Render(w, r, ErrInvalidRequest(nil))
		return
	}
	if sched.InventorySourceID != nil {
		inventoryID, found := rs.store.InventoryIDBySource(r.Context(), *sched.InventorySourceID)
		if !found || !rs.authorize(w, r, rbac.Inventory, inventoryID, actUpdate) {
			return
		}
	} else if newOrg != curOrg {
		if !rs.authorize(w, r, rbac.Organization, newOrg, actAdmin) {
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
	actorID := currentUser(r).UserID
	sched.ActorUserID = &actorID
	if len(sched.ExtraVars) == 0 {
		sched.ExtraVars = json.RawMessage("{}")
	}

	if err := rs.store.Update(r.Context(), sched); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	render.JSON(w, r, dto.FromSchedule(sched))
}

func (rs *SchedulesResource) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	sched, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.Render(w, r, ErrNotFound)
		return
	}
	if sched.InventorySourceID != nil {
		inventoryID, ok := rs.store.InventoryIDBySource(r.Context(), *sched.InventorySourceID)
		if !ok || !rs.authorize(w, r, rbac.Inventory, inventoryID, actUpdate) {
			return
		}
		if err := rs.store.Delete(r.Context(), id); err != nil {
			render.Render(w, r, ErrInternal(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	org, ok := rs.store.ScheduleOrg(r.Context(), id)
	if !ok {
		render.Render(w, r, ErrNotFound)
		return
	}
	if !rs.authorize(w, r, rbac.Organization, org, actAdmin) {
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
