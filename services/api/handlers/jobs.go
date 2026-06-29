package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
)

type JobsResource struct {
	DB *sqlx.DB
	*Authorizer
}

func NewJobsResource(db *sqlx.DB) *JobsResource {
	return &JobsResource{DB: db, Authorizer: NewAuthorizer(db)}
}

// templateIDForRun resolves the job_templates.id that owns a given execution
// run, via unified_job -> unified_job_template_id. ok is false when the run has
// no governing template (e.g. an ad-hoc job).
func (rs *JobsResource) templateIDForRun(r *http.Request, runID uuid.UUID) (int64, bool) {
	var jtID int64
	err := rs.DB.GetContext(r.Context(), &jtID, `
		SELECT jt.id
		FROM execution_runs er
		JOIN unified_jobs uj ON er.unified_job_id = uj.id
		JOIN job_templates jt ON uj.unified_job_template_id = jt.unified_job_template_id
		WHERE er.id = $1`, runID)
	return jtID, err == nil
}

// authorizeRunRead allows reading a run/its events when the user can read the
// governing template; runs with no template are visible only to superuser/auditor.
func (rs *JobsResource) authorizeRunRead(w http.ResponseWriter, r *http.Request, runID uuid.UUID) bool {
	if jtID, ok := rs.templateIDForRun(r, runID); ok {
		return rs.authorize(w, r, rbac.ContentTypeJobTemplate, jtID, actRead)
	}
	uc := currentUser(r)
	if uc.IsSuperuser || uc.IsSystemAuditor {
		return true
	}
	render.Render(w, r, ErrForbidden)
	return false
}

// Routes creates a REST router for the Jobs resource
func (rs *JobsResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListUnifiedJobs)
	r.Post("/", rs.LaunchJob)
	r.Get("/runs/{runID}", rs.GetExecutionRun)
	r.Get("/runs/{runID}/events", rs.ListJobEvents)
	r.Post("/runs/{runID}/events", rs.CreateJobEvent)
	return r
}

// ListUnifiedJobs returns a list of unified jobs
func (rs *JobsResource) ListUnifiedJobs(w http.ResponseWriter, r *http.Request) {
	uc := currentUser(r)
	jobs := []models.UnifiedJob{}

	if uc.IsSuperuser || uc.IsSystemAuditor {
		if err := rs.DB.SelectContext(r.Context(), &jobs, `SELECT * FROM unified_jobs ORDER BY created_at DESC LIMIT 50`); err != nil {
			render.Render(w, r, ErrInternal(err))
			return
		}
		render.JSON(w, r, jobs)
		return
	}

	// Regular users see only jobs whose governing template they can read.
	ids, err := rs.readableIDs(r, rbac.ContentTypeJobTemplate)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	if len(ids) > 0 {
		q, args, _ := sqlx.In(`
			SELECT uj.* FROM unified_jobs uj
			JOIN job_templates jt ON uj.unified_job_template_id = jt.unified_job_template_id
			WHERE jt.id IN (?)
			ORDER BY uj.created_at DESC LIMIT 50`, ids)
		q = rs.DB.Rebind(q)
		if err := rs.DB.SelectContext(r.Context(), &jobs, q, args...); err != nil {
			render.Render(w, r, ErrInternal(err))
			return
		}
	}
	render.JSON(w, r, jobs)
}

// LaunchJob creates a new unified job with status 'pending'
// The Scheduler will pick this up, create an execution_run, and dispatch it.
func (rs *JobsResource) LaunchJob(w http.ResponseWriter, r *http.Request) {
	// Simple launch payload
	type LaunchRequest struct {
		UnifiedJobTemplateID int64  `json:"unified_job_template_id"`
		Name                 string `json:"name"`
	}
	var req LaunchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Launching requires execute access on the job template. The launch payload
	// carries the unified_job_template_id; map it to the job_templates row that
	// owns the roles.
	var jtID int64
	if err := rs.DB.GetContext(r.Context(), &jtID,
		`SELECT id FROM job_templates WHERE unified_job_template_id = $1`, req.UnifiedJobTemplateID); err != nil {
		render.Render(w, r, ErrInvalidRequest(fmt.Errorf("unknown job template")))
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, jtID, actExecute) {
		return
	}

	// Insert unified_job in 'pending' state with NO current_run_id
	// The Scheduler will:
	// 1. Pick up jobs WHERE status='pending' AND current_run_id IS NULL
	// 2. Create execution_run
	// 3. Set current_run_id and status='queued'
	// 4. Publish to NATS for execution
	var jobID int64
	err := rs.DB.QueryRowContext(r.Context(), `
		INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at)
		VALUES ($1, $2, 'pending', $3)
		RETURNING id`,
		req.Name, req.UnifiedJobTemplateID, time.Now(),
	).Scan(&jobID)

	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	// Return created job - scheduler will assign current_run_id shortly
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, map[string]interface{}{
		"id":     jobID,
		"status": "pending",
	})
}

// GetExecutionRun returns details of a specific execution run
func (rs *JobsResource) GetExecutionRun(w http.ResponseWriter, r *http.Request) {
	runIDStr := chi.URLParam(r, "runID")
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	if !rs.authorizeRunRead(w, r, runID) {
		return
	}

	var run models.ExecutionRun
	err = rs.DB.GetContext(r.Context(), &run, `SELECT * FROM execution_runs WHERE id = $1`, runID)
	if err == sql.ErrNoRows {
		render.Render(w, r, ErrNotFound)
		return
	} else if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	render.JSON(w, r, run)
}

// ListJobEvents returns all events for a specific execution run
func (rs *JobsResource) ListJobEvents(w http.ResponseWriter, r *http.Request) {
	runIDStr := chi.URLParam(r, "runID")
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	if !rs.authorizeRunRead(w, r, runID) {
		return
	}

	query := `SELECT * FROM job_events WHERE execution_run_id = $1 ORDER BY seq ASC`
	events := []models.JobEvent{}
	if err := rs.DB.SelectContext(r.Context(), &events, query, runID); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, events)
}

// CreateJobEvent ingests a new event (used by host-runner)
func (rs *JobsResource) CreateJobEvent(w http.ResponseWriter, r *http.Request) {
	runIDStr := chi.URLParam(r, "runID")
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	var evt models.JobEvent
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// 1. Look up UnifiedJobID from ExecutionRun
	var unifiedJobID int64
	err = rs.DB.QueryRowContext(r.Context(), `SELECT unified_job_id FROM execution_runs WHERE id = $1`, runID).Scan(&unifiedJobID)
	if err != nil {
		if err == sql.ErrNoRows {
			render.Render(w, r, ErrNotFound)
			return
		}
		render.Render(w, r, ErrInternal(err))
		return
	}

	// 2. Insert Event
	query := `
		INSERT INTO job_events (
			unified_job_id, execution_run_id, seq, event_type, 
			stdout_snippet, event_data, created_at
		) VALUES (
			$1, $2, $3, $4, 
			$5, $6, $7
		) RETURNING id`

	// Ensure defaults
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = time.Now()
	}
	if evt.EventData == nil {
		evt.EventData = json.RawMessage("{}")
	}

	var newID int64
	err = rs.DB.QueryRowContext(r.Context(), query,
		unifiedJobID, runID, evt.Seq, evt.EventType,
		evt.StdoutSnippet, evt.EventData, evt.CreatedAt,
	).Scan(&newID)

	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}

	evt.ID = newID
	evt.UnifiedJobID = unifiedJobID
	evt.ExecutionRunID = runID

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, evt)
}

// -- Err Helpers (Basic) --

func ErrInternal(err error) render.Renderer {
	return &ErrResponse{
		Err:            err,
		HTTPStatusCode: 500,
		StatusText:     "Internal Server Error",
		ErrorText:      err.Error(),
	}
}

func ErrInvalidRequest(err error) render.Renderer {
	return &ErrResponse{
		Err:            err,
		HTTPStatusCode: 400,
		StatusText:     "Invalid Request",
		ErrorText:      err.Error(),
	}
}

var ErrNotFound = &ErrResponse{HTTPStatusCode: 404, StatusText: "Resource not found"}
var ErrForbidden = &ErrResponse{HTTPStatusCode: 403, StatusText: "Forbidden"}

type ErrResponse struct {
	Err            error  `json:"-"`
	HTTPStatusCode int    `json:"-"`
	StatusText     string `json:"status"`
	AppCode        int64  `json:"code,omitempty"`
	ErrorText      string `json:"error,omitempty"`
}

func (e *ErrResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.HTTPStatusCode)
	return nil
}
