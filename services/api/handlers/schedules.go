package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/teambition/rrule-go"
)

type SchedulesResource struct {
	DB *sqlx.DB
}

func NewSchedulesResource(db *sqlx.DB) *SchedulesResource {
	return &SchedulesResource{DB: db}
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
	schedules := []models.Schedule{}
	err := rs.DB.SelectContext(r.Context(), &schedules, "SELECT * FROM schedules ORDER BY id ASC")
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, schedules)
}

func (rs *SchedulesResource) GetSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
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

	// Validation
	if sched.Name == "" || sched.RRule == "" || sched.UnifiedJobTemplateID == 0 {
		render.Render(w, r, ErrInvalidRequest(nil)) // Todo: better error
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

	query := `
		INSERT INTO schedules (name, description, unified_job_template_id, rrule, next_run, enabled, extra_vars, created_at, modified_at)
		VALUES (:name, :description, :unified_job_template_id, :rrule, :next_run, :enabled, :extra_vars, :created_at, :modified_at)
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

	var sched models.Schedule
	if err := json.NewDecoder(r.Body).Decode(&sched); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}
	sched.ID = id

	// Validate RRULE if changed (simplified: always validate)
	rule, err := rrule.StrToRRule(sched.RRule)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Recalculate NextRun
	sched.NextRun = rule.After(time.Now(), false)
	sched.ModifiedAt = time.Now()

	query := `
		UPDATE schedules 
		SET name=:name, description=:description, rrule=:rrule, next_run=:next_run, 
		    enabled=:enabled, extra_vars=:extra_vars, modified_at=:modified_at,
		    unified_job_template_id=:unified_job_template_id
		WHERE id = :id`

	if _, err := rs.DB.NamedExecContext(r.Context(), query, sched); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	render.JSON(w, r, sched)
}

func (rs *SchedulesResource) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := rs.DB.ExecContext(r.Context(), "DELETE FROM schedules WHERE id = $1", id)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -- Err Helpers --
// Note: ErrInternal, ErrInvalidRequest, etc. are defined in jobs.go which is in the same package.
