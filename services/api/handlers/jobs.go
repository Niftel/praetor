package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/launch"
	"github.com/praetordev/models"
	"github.com/praetordev/plog"
	"github.com/praetordev/rbac"
	"github.com/praetordev/praetor/services/api/store"
)

// JobStore is the jobs-domain data access the handler depends on (implemented by
// services/api/store.JobStore). Declared here, consumer-side, so the handler owns
// its data contract and stays testable with a fake.
type JobStore interface {
	// reads
	ListRecent(ctx context.Context, limit int) ([]models.UnifiedJob, error)
	ListReadable(ctx context.Context, tmplIDs []int64, limit int) ([]models.UnifiedJob, error)
	GetRun(ctx context.Context, runID uuid.UUID) (models.ExecutionRun, error)
	ListEvents(ctx context.Context, runID uuid.UUID) ([]models.JobEvent, error)
	TemplateIDForRun(ctx context.Context, runID uuid.UUID) (int64, bool, error)
	// writes
	LaunchTemplateInfo(ctx context.Context, unifiedTemplateID int64) (store.LaunchTemplateInfo, error)
	ActiveJobCount(ctx context.Context, unifiedTemplateID int64) (int, error)
	InsertPendingJob(ctx context.Context, name string, unifiedTemplateID int64, opts launch.Options) (int64, error)
	UnifiedJobIDForRun(ctx context.Context, runID uuid.UUID) (int64, error)
	InsertJobEvent(ctx context.Context, evt *models.JobEvent) (int64, error)
	JobCancelInfo(ctx context.Context, jobID int64) (store.JobCancelInfo, error)
	JobTemplateIDByUnified(ctx context.Context, unifiedTemplateID int64) (int64, bool, error)
	FlagCancelRequested(ctx context.Context, jobID int64) error
	CancelNotYetRunning(ctx context.Context, jobID int64) error
}

type JobsResource struct {
	DB *sqlx.DB
	*Authorizer
	// IngestionURL is the base URL the API proxies run-log reads to. Resolved in
	// main from env; empty falls back to the in-cluster default.
	IngestionURL string
	// internalToken is the shared cluster secret the API presents to ingestion's
	// authenticated log-read endpoint (the run-scoped GET logs is no longer open).
	internalToken string
	store         JobStore
	log           *slog.Logger
}

func NewJobsResource(db *sqlx.DB, ingestionURL, internalToken string) *JobsResource {
	return &JobsResource{
		DB:            db,
		Authorizer:    NewAuthorizer(db),
		IngestionURL:  ingestionURL,
		internalToken: internalToken,
		store:         store.NewJobStore(db),
		log:           plog.New("api.jobs"),
	}
}

// authorizeRunRead allows reading a run/its events when the user can read the
// governing template; runs with no template are visible only to superuser/auditor.
// A real DB error fails the request closed with a 500 rather than being masked
// as an unowned run.
func (rs *JobsResource) authorizeRunRead(w http.ResponseWriter, r *http.Request, runID uuid.UUID) bool {
	jtID, ok, err := rs.store.TemplateIDForRun(r.Context(), runID)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return false
	}
	if ok {
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
	jt, err := rs.store.LaunchTemplateInfo(r.Context(), req.UnifiedJobTemplateID)
	if err != nil {
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
		if active, err := rs.store.ActiveJobCount(r.Context(), req.UnifiedJobTemplateID); err == nil && active > 0 {
			render.Render(w, r, ErrConflict(fmt.Errorf("a run of this job template is already active; wait for it to finish or enable Allow Simultaneous")))
			return
		}
	}

	// Collect launch overrides, accepting each only if the template opts in.
	// A survey, when enabled, is the variable-prompt mechanism: answers are
	// validated against the spec (defaults filled, required enforced) and become
	// extra_vars regardless of ask_variables_on_launch. Otherwise a plain
	// variables prompt is honored only if the template asks for it.
	var opts launch.Options
	if jt.SurveyEnabled {
		answers, serr := applySurvey(jt.SurveySpec, req.ExtraVars)
		if serr != nil {
			render.Render(w, r, ErrInvalidRequest(serr))
			return
		}
		opts.ExtraVars = answers
	} else if jt.AskVariablesOnLaunch && len(req.ExtraVars) > 0 {
		opts.ExtraVars = req.ExtraVars
	}
	if jt.AskLimitOnLaunch && req.Limit != nil {
		opts.Limit = req.Limit
	}

	// Insert unified_job in 'pending' state with NO current_run_id
	// The Scheduler will:
	// 1. Pick up jobs WHERE status='pending' AND current_run_id IS NULL
	// 2. Create execution_run
	// 3. Set current_run_id and status='queued'
	// 4. Publish to NATS for execution
	jobID, err := rs.store.InsertPendingJob(r.Context(), req.Name, req.UnifiedJobTemplateID, opts)
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
	if errors.Is(err, sql.ErrNoRows) {
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
	// ingestion's log-read endpoint is authenticated (in-cluster); present the
	// shared internal token. Edge RBAC already happened above via authorizeRunRead.
	if rs.internalToken != "" {
		req.Header.Set("Authorization", "Bearer "+rs.internalToken)
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
	unifiedJobID, err := rs.store.UnifiedJobIDForRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			render.Render(w, r, ErrNotFound)
			return
		}
		render.Render(w, r, ErrInternal(err))
		return
	}

	// 2. Insert Event (fill defaults first)
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = time.Now()
	}
	if evt.EventData == nil {
		evt.EventData = json.RawMessage("{}")
	}
	evt.UnifiedJobID = unifiedJobID
	evt.ExecutionRunID = runID

	newID, err := rs.store.InsertJobEvent(r.Context(), &evt)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	evt.ID = newID

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
	job, err := rs.store.JobCancelInfo(r.Context(), id)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(fmt.Errorf("unknown job")))
		return
	}
	// Cancelling requires execute access on the governing template.
	if job.UnifiedJobTemplateID != nil {
		if jtID, ok, err := rs.store.JobTemplateIDByUnified(r.Context(), *job.UnifiedJobTemplateID); err == nil && ok {
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
		if err := rs.store.FlagCancelRequested(r.Context(), id); err != nil {
			rs.log.Error("flag cancel requested failed", "job_id", id, "err", err)
			render.Render(w, r, ErrInternal(err))
			return
		}
		render.JSON(w, r, map[string]string{"status": "canceling"})
		return
	default:
		// pending / queued / waiting: not executing yet — cancel outright so it's
		// never dispatched, and terminate any run row already created for it.
		if err := rs.store.CancelNotYetRunning(r.Context(), id); err != nil {
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
