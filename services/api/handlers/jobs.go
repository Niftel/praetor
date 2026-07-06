package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/store"
)

// JobStore is the jobs-domain data access the handler depends on (implemented by
// services/api/store.JobStore). Declared here, consumer-side, so the handler owns
// its data contract and stays testable with a fake.
type JobStore interface {
	ListRecent(ctx context.Context, limit int) ([]models.UnifiedJob, error)
	ListReadable(ctx context.Context, tmplIDs []int64, limit int) ([]models.UnifiedJob, error)
	GetRun(ctx context.Context, runID uuid.UUID) (models.ExecutionRun, error)
	ListEvents(ctx context.Context, runID uuid.UUID) ([]models.JobEvent, error)
	TemplateIDForRun(ctx context.Context, runID uuid.UUID) (int64, bool, error)
}

type JobsResource struct {
	DB *sqlx.DB
	*Authorizer
	// IngestionURL is the base URL the API proxies run-log reads to. Resolved in
	// main from env; empty falls back to the in-cluster default.
	IngestionURL string
	store        JobStore
}

func NewJobsResource(db *sqlx.DB, ingestionURL string) *JobsResource {
	return &JobsResource{DB: db, Authorizer: NewAuthorizer(db), IngestionURL: ingestionURL, store: store.NewJobStore(db)}
}

// templateIDForRun resolves the job_templates.id that owns a given execution
// run. ok is false when the run has no governing template (e.g. an ad-hoc job).
func (rs *JobsResource) templateIDForRun(r *http.Request, runID uuid.UUID) (int64, bool) {
	id, ok, _ := rs.store.TemplateIDForRun(r.Context(), runID)
	return id, ok
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
	r.Post("/{id}/cancel", rs.CancelJob)
	r.Get("/runs/{runID}", rs.GetExecutionRun)
	r.Get("/runs/{runID}/events", rs.ListJobEvents)
	r.Post("/runs/{runID}/events", rs.CreateJobEvent)
	r.Get("/runs/{runID}/logs", rs.StreamRunLogs)
	return r
}

// ListUnifiedJobs returns a list of unified jobs
func (rs *JobsResource) ListUnifiedJobs(w http.ResponseWriter, r *http.Request) {
	uc := currentUser(r)

	if uc.IsSuperuser || uc.IsSystemAuditor {
		jobs, err := rs.store.ListRecent(r.Context(), 50)
		if err != nil {
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
	jobs, err := rs.store.ListReadable(r.Context(), ids, 50)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, jobs)
}

// LaunchJob creates a new unified job with status 'pending'
// The Scheduler will pick this up, create an execution_run, and dispatch it.
func (rs *JobsResource) LaunchJob(w http.ResponseWriter, r *http.Request) {
	// Launch payload: the template id, an optional name, and prompt-on-launch
	// overrides (only honored when the template opts in via its ask_* flags).
	type LaunchRequest struct {
		UnifiedJobTemplateID int64                  `json:"unified_job_template_id"`
		Name                 string                 `json:"name"`
		ExtraVars            map[string]interface{} `json:"extra_vars,omitempty"`
		Limit                *string                `json:"limit,omitempty"`
	}
	var req LaunchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Launching requires execute access on the job template. The launch payload
	// carries the unified_job_template_id; map it to the job_templates row that
	// owns the roles, and read its prompt-on-launch flags.
	var jt struct {
		ID                   int64           `db:"id"`
		AskVariablesOnLaunch bool            `db:"ask_variables_on_launch"`
		AskLimitOnLaunch     bool            `db:"ask_limit_on_launch"`
		SurveyEnabled        bool            `db:"survey_enabled"`
		SurveySpec           json.RawMessage `db:"survey_spec"`
		AllowSimultaneous    bool            `db:"allow_simultaneous"`
	}
	if err := rs.DB.GetContext(r.Context(), &jt,
		`SELECT id, ask_variables_on_launch, ask_limit_on_launch, survey_enabled, survey_spec, allow_simultaneous
		 FROM job_templates WHERE unified_job_template_id = $1`, req.UnifiedJobTemplateID); err != nil {
		render.Render(w, r, ErrInvalidRequest(fmt.Errorf("unknown job template")))
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, jt.ID, actExecute) {
		return
	}

	// Concurrency guard: unless the template opts into simultaneous runs, refuse a
	// launch while a prior run of the same template is still active. Stops
	// accidental double-triggers from queuing a second overlapping run.
	if !jt.AllowSimultaneous {
		var active int
		if err := rs.DB.GetContext(r.Context(), &active,
			`SELECT count(*) FROM unified_jobs
			 WHERE unified_job_template_id = $1 AND status NOT IN ('successful','failed','canceled','error')`,
			req.UnifiedJobTemplateID); err == nil && active > 0 {
			render.Render(w, r, ErrConflict(fmt.Errorf("a run of this job template is already active; wait for it to finish or enable Allow Simultaneous")))
			return
		}
	}

	// Collect launch overrides, accepting each only if the template opts in.
	// A survey, when enabled, is the variable-prompt mechanism: answers are
	// validated against the spec (defaults filled, required enforced) and become
	// extra_vars regardless of ask_variables_on_launch. Otherwise a plain
	// variables prompt is honored only if the template asks for it.
	overrides := map[string]interface{}{}
	if jt.SurveyEnabled {
		answers, serr := applySurvey(jt.SurveySpec, req.ExtraVars)
		if serr != nil {
			render.Render(w, r, ErrInvalidRequest(serr))
			return
		}
		overrides["extra_vars"] = answers
	} else if jt.AskVariablesOnLaunch && len(req.ExtraVars) > 0 {
		overrides["extra_vars"] = req.ExtraVars
	}
	if jt.AskLimitOnLaunch && req.Limit != nil {
		overrides["limit"] = *req.Limit
	}
	jobArgs := []byte("{}")
	if len(overrides) > 0 {
		if b, err := json.Marshal(overrides); err == nil {
			jobArgs = b
		}
	}

	// Insert unified_job in 'pending' state with NO current_run_id
	// The Scheduler will:
	// 1. Pick up jobs WHERE status='pending' AND current_run_id IS NULL
	// 2. Create execution_run
	// 3. Set current_run_id and status='queued'
	// 4. Publish to NATS for execution
	var jobID int64
	err := rs.DB.QueryRowContext(r.Context(), `
		INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at, job_args)
		VALUES ($1, $2, 'pending', $3, $4)
		RETURNING id`,
		req.Name, req.UnifiedJobTemplateID, time.Now(), jobArgs,
	).Scan(&jobID)

	if err != nil {
		// Lost the race to a concurrent launch of a non-simultaneous template.
		if isActiveRunConflict(err) {
			render.Render(w, r, ErrConflict(fmt.Errorf("a run of this job template is already active; wait for it to finish or enable Allow Simultaneous")))
			return
		}
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

	run, err := rs.store.GetRun(r.Context(), runID)
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

	events, err := rs.store.ListEvents(r.Context(), runID)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	render.JSON(w, r, events)
}

// StreamRunLogs returns a run's full stdout. Bulk playbook output is streamed to
// the object store (not the event stream), so this proxies the ingestion
// service's reassembled-log endpoint rather than reading job_events. Auth
// matches ListJobEvents (the user must be able to read the governing template).
func (rs *JobsResource) StreamRunLogs(w http.ResponseWriter, r *http.Request) {
	runIDStr := chi.URLParam(r, "runID")
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}
	if !rs.authorizeRunRead(w, r, runID) {
		return
	}

	base := rs.IngestionURL
	if base == "" {
		base = "http://ingestion:8081"
	}
	// The upstream chunk query is exclusive (seq > since) and chunks start at
	// seq 0, so a full fetch must pass since=-1 — defaulting to 0 would silently
	// drop the first chunk (the start of the playbook output).
	since := r.URL.Query().Get("since")
	if since == "" {
		since = "-1"
	}
	upstream := fmt.Sprintf("%s/api/v1/runs/%s/logs?since=%s", base, runID, url.QueryEscape(since))

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		render.Render(w, r, ErrInternal(fmt.Errorf("reach ingestion logs: %w", err)))
		return
	}
	defer resp.Body.Close()

	// Forward the tail cursor so the client can poll incrementally: it passes the
	// value back as ?since= to fetch only chunks newer than what it already has.
	if ls := resp.Header.Get("X-Praetor-Last-Seq"); ls != "" {
		w.Header().Set("X-Praetor-Last-Seq", ls)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
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

// CancelJob POST /api/v1/jobs/{id}/cancel — request cancellation of a job.
// A running job is flagged (cancel_requested); its host-runner sees the flag on
// its next heartbeat and stops the play cooperatively, emitting JOB_CANCELED. A
// job that hasn't started executing yet (pending/queued) is canceled outright.
func (rs *JobsResource) CancelJob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}
	var job struct {
		Status string `db:"status"`
		UJTID  *int64 `db:"unified_job_template_id"`
	}
	if err := rs.DB.GetContext(r.Context(), &job,
		`SELECT status, unified_job_template_id FROM unified_jobs WHERE id = $1`, id); err != nil {
		render.Render(w, r, ErrInvalidRequest(fmt.Errorf("unknown job")))
		return
	}
	// Cancelling requires execute access on the governing template.
	if job.UJTID != nil {
		var jtID int64
		if err := rs.DB.GetContext(r.Context(), &jtID,
			`SELECT id FROM job_templates WHERE unified_job_template_id = $1`, *job.UJTID); err == nil {
			if !rs.authorize(w, r, rbac.ContentTypeJobTemplate, jtID, actExecute) {
				return
			}
		}
	}
	switch job.Status {
	case "successful", "failed", "canceled", "error":
		render.Render(w, r, ErrConflict(fmt.Errorf("job already finished (%s)", job.Status)))
		return
	case "running":
		// Executing on a host: flag it; the host-runner stops the play on its next
		// heartbeat and reports JOB_CANCELED, which finalizes the state.
		rs.DB.ExecContext(r.Context(), `UPDATE unified_jobs SET cancel_requested = true WHERE id = $1`, id)
		render.JSON(w, r, map[string]string{"status": "canceling"})
		return
	default:
		// pending / queued / waiting: not executing yet — cancel outright so it's
		// never dispatched, and terminate any run row already created for it.
		tx, err := rs.DB.Beginx()
		if err != nil {
			render.Render(w, r, ErrInternal(err))
			return
		}
		defer tx.Rollback()
		tx.ExecContext(r.Context(), `UPDATE unified_jobs SET cancel_requested = true, status = 'canceled', finished_at = now() WHERE id = $1`, id)
		tx.ExecContext(r.Context(), `UPDATE execution_runs SET state = 'canceled', finished_at = now() WHERE unified_job_id = $1 AND state NOT IN ('successful','failed','canceled')`, id)
		if err := tx.Commit(); err != nil {
			render.Render(w, r, ErrInternal(err))
			return
		}
		render.JSON(w, r, map[string]string{"status": "canceled"})
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

// ErrConflict (409) — the launch clashes with current state (e.g. a prior run of
// the same template is still active).
func ErrConflict(err error) render.Renderer {
	return &ErrResponse{
		Err:            err,
		HTTPStatusCode: 409,
		StatusText:     "Conflict",
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
