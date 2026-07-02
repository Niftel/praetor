package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/teambition/rrule-go"
)

type SchedulesResource struct {
	DB *sqlx.DB
	*Authorizer
}

func NewSchedulesResource(db *sqlx.DB) *SchedulesResource {
	return &SchedulesResource{DB: db, Authorizer: NewAuthorizer(db)}
}

// A schedule has no org column; it inherits its target's org. These helpers
// resolve that org so schedule access can be gated on the target's organization.
func (rs *SchedulesResource) targetOrg(ctx context.Context, wfID, ujtID *int64) (int64, bool) {
	var org int64
	switch {
	case wfID != nil:
		err := rs.DB.GetContext(ctx, &org, `SELECT organization_id FROM workflow_templates WHERE id=$1`, *wfID)
		return org, err == nil
	case ujtID != nil:
		err := rs.DB.GetContext(ctx, &org, `SELECT organization_id FROM job_templates WHERE unified_job_template_id=$1`, *ujtID)
		return org, err == nil
	}
	return 0, false
}

func (rs *SchedulesResource) scheduleOrg(ctx context.Context, id int64) (int64, bool) {
	var org sql.NullInt64
	err := rs.DB.GetContext(ctx, &org, `
		SELECT COALESCE(wt.organization_id, jt.organization_id)
		FROM schedules s
		LEFT JOIN workflow_templates wt ON wt.id = s.workflow_template_id
		LEFT JOIN job_templates jt ON jt.unified_job_template_id = s.unified_job_template_id
		WHERE s.id=$1`, id)
	if err != nil || !org.Valid {
		return 0, false
	}
	return org.Int64, true
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
	schedules := []models.Schedule{}
	if uc.IsSuperuser || uc.IsSystemAuditor {
		if err := rs.DB.SelectContext(r.Context(), &schedules, "SELECT * FROM schedules ORDER BY id ASC"); err != nil {
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
	if len(ids) > 0 {
		q, args, _ := sqlx.In(`
			SELECT s.* FROM schedules s
			LEFT JOIN workflow_templates wt ON wt.id = s.workflow_template_id
			LEFT JOIN job_templates jt ON jt.unified_job_template_id = s.unified_job_template_id
			WHERE COALESCE(wt.organization_id, jt.organization_id) IN (?)
			ORDER BY s.id ASC`, ids)
		q = rs.DB.Rebind(q)
		if err := rs.DB.SelectContext(r.Context(), &schedules, q, args...); err != nil {
			render.Render(w, r, ErrInternal(err))
			return
		}
	}
	render.JSON(w, r, schedules)
}

func (rs *SchedulesResource) GetSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if org, ok := rs.scheduleOrg(r.Context(), id); !ok {
		render.Render(w, r, ErrNotFound)
		return
	} else if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actRead) {
		return
	}
	var sched models.Schedule
	err := rs.DB.GetContext(r.Context(), &sched, "SELECT * FROM schedules WHERE id = $1", id)
	if err == sql.ErrNoRows {
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
	org, ok := rs.targetOrg(r.Context(), sched.WorkflowTemplateID, sched.UnifiedJobTemplateID)
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

	query := `
		INSERT INTO schedules (name, description, unified_job_template_id, workflow_template_id, rrule, next_run, enabled, extra_vars, created_at, modified_at)
		VALUES (:name, :description, :unified_job_template_id, :workflow_template_id, :rrule, :next_run, :enabled, :extra_vars, :created_at, :modified_at)
		RETURNING id`

	rows, err := rs.DB.NamedQuery(query, sched)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	if rows.Next() {
		rows.Scan(&sched.ID)
	}
	rows.Close()

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
	curOrg, ok := rs.scheduleOrg(r.Context(), id)
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
	if newOrg, ok := rs.targetOrg(r.Context(), sched.WorkflowTemplateID, sched.UnifiedJobTemplateID); ok && newOrg != curOrg {
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

	query := `
		UPDATE schedules
		SET name=:name, description=:description, rrule=:rrule, next_run=:next_run,
		    enabled=:enabled, extra_vars=:extra_vars, modified_at=:modified_at,
		    unified_job_template_id=:unified_job_template_id,
		    workflow_template_id=:workflow_template_id
		WHERE id = :id`

	if _, err := rs.DB.NamedExecContext(r.Context(), query, sched); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	render.JSON(w, r, sched)
}

func (rs *SchedulesResource) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	org, ok := rs.scheduleOrg(r.Context(), id)
	if !ok {
		render.Render(w, r, ErrNotFound)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, org, actAdmin) {
		return
	}
	_, err := rs.DB.ExecContext(r.Context(), "DELETE FROM schedules WHERE id = $1", id)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -- Err Helpers --
// Note: ErrInternal, ErrInvalidRequest, etc. are defined in jobs.go which is in the same package.
