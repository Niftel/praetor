package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/praetordev/praetor/pkg/models"
	praetorRender "github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/ingestion/core"
)

type IngestionHandler struct {
	Service *core.IngestionService
}

func NewIngestionHandler(svc *core.IngestionService) *IngestionHandler {
	return &IngestionHandler{Service: svc}
}

// Ingest POST /api/v1/runs/{run_id}/events
func (h *IngestionHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	runIDStr := chi.URLParam(r, "run_id")
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var events []models.JobEvent
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if err := h.Service.IngestEvents(r.Context(), runID, events); err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, map[string]string{"status": "accepted"})
}

// IngestLog POST /api/v1/runs/{run_id}/logs?seq=N
// The request body is the raw stdout chunk; it is stored in the object store and
// indexed in job_output_chunks.
func (h *IngestionHandler) IngestLog(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}

	seq, err := strconv.ParseInt(r.URL.Query().Get("seq"), 10, 64)
	if err != nil {
		praetorRender.ErrInvalidRequest(fmt.Errorf("invalid or missing seq: %w", err)).Render(w, r)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if err := h.Service.IngestLogChunk(r.Context(), runID, seq, data); err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, map[string]string{"status": "accepted"})
}

// Heartbeat POST /api/v1/runs/{run_id}/heartbeat — called by the host-runner
// during execution to stamp execution_runs.last_heartbeat_at.
func (h *IngestionHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := h.Service.RecordHeartbeat(r.Context(), runID); err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, map[string]string{"status": "ok"})
}

// StreamLog GET /api/v1/runs/{run_id}/logs?since=N
// Streams the run's stored stdout (chunks reassembled in order) back to the
// caller. `since` supports incremental tailing; the highest seq written is
// returned in the X-Praetor-Last-Seq header so a poller can advance its cursor.
func (h *IngestionHandler) StreamLog(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// since is the highest seq the caller already has; -1 (the default) fetches
	// from the beginning. The query is exclusive (seq > since).
	since := int64(-1)
	if v := r.URL.Query().Get("since"); v != "" {
		if since, err = strconv.ParseInt(v, 10, 64); err != nil {
			praetorRender.ErrInvalidRequest(fmt.Errorf("invalid since: %w", err)).Render(w, r)
			return
		}
	}

	// Resolve the tail cursor up front so it can be sent as a header before the
	// body is written (headers can't be set once streaming has begun).
	latest, err := h.Service.LatestLogSeq(r.Context(), runID)
	if err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	if latest < since {
		latest = since
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Praetor-Last-Seq", strconv.FormatInt(latest, 10))

	if err := h.Service.StreamLogs(r.Context(), runID, since, w); err != nil {
		// The body may be partially written at this point; the error is logged
		// by the renderer. Nothing more we can safely do mid-stream.
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
}
