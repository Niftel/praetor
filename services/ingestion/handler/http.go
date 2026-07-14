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
	"github.com/praetordev/models"
	praetorRender "github.com/praetordev/render"
	"github.com/praetordev/praetor/services/ingestion/core"
)

type IngestionHandler struct {
	Service *core.IngestionService
}

func NewIngestionHandler(svc *core.IngestionService) *IngestionHandler {
	return &IngestionHandler{Service: svc}
}

// Runnable GET /api/v1/runs/{run_id}/runnable — the executor's pre-bootstrap gate.
// Returns {"runnable": bool}; false means the run is terminal/absent and must not
// be bootstrapped (prevents a reaped/canceled launch from running as a ghost).
func (h *IngestionHandler) Runnable(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	ok, err := h.Service.IsRunnable(r.Context(), runID)
	if err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, map[string]bool{"runnable": ok})
}

// InventoryRendered GET /internal/v1/inventories/{id}/rendered — the executor
// fetches the rendered INI at dispatch (the manifest ships only the inventory id,
// #13). Authenticated with the internal token (in-cluster caller).
func (h *IngestionHandler) InventoryRendered(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	ini, err := h.Service.RenderInventory(r.Context(), id)
	if err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(ini))
}

// InventoryFacts GET /internal/v1/inventories/{id}/facts — stored host facts for a
// fact-cache job, fetched by reference at dispatch (#48). Internal-token auth.
func (h *IngestionHandler) InventoryFacts(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	facts, err := h.Service.InventoryFacts(r.Context(), id)
	if err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	if facts == nil {
		facts = map[string]json.RawMessage{}
	}
	render.JSON(w, r, facts)
}

// LogCursor GET /api/v1/runs/{run_id}/logs/cursor — the host-runner's stdout
// resume point: {"bytes": <total stored>, "seq": <max seq, -1 if none>}. Used to
// recover after a lost local sync cursor without re-chunking stdout from 0 (#9).
func (h *IngestionHandler) LogCursor(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	bytes, seq, err := h.Service.LogCursor(r.Context(), runID)
	if err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, map[string]int64{"bytes": bytes, "seq": seq})
}

// ResolveCredentials GET /internal/v1/runs/{run_id}/credentials — returns the
// decrypted injectors for the run's snapshotted Machine credential. Authenticated
// (internal token) and run-scoped; the response is never logged.
func (h *IngestionHandler) ResolveCredentials(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	env, files, err := h.Service.ResolveRunCredentials(r.Context(), runID)
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	render.JSON(w, r, map[string]map[string]string{"env": env, "files": files})
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
	cancel, err := h.Service.RecordHeartbeat(r.Context(), runID)
	if err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, map[string]interface{}{"status": "ok", "cancel": cancel})
}

// IngestFacts POST /api/v1/runs/{run_id}/facts — host-runner ships the facts
// Ansible gathered; they're upserted into host_facts (keyed by host).
func (h *IngestionHandler) IngestFacts(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	var body struct {
		Facts map[string]json.RawMessage `json:"facts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := h.Service.StoreFacts(r.Context(), runID, body.Facts); err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, map[string]string{"status": "accepted"})
}

// IngestInventorySync POST /api/v1/inventories/{id}/sync-data — body is the
// `ansible-inventory --list` JSON; it's upserted into the inventory.
func (h *IngestionHandler) IngestInventorySync(w http.ResponseWriter, r *http.Request) {
	invID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 32<<20)) // cap 32MB
	if err != nil {
		praetorRender.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := h.Service.UpsertInventory(r.Context(), invID, data); err != nil {
		praetorRender.ErrInternal(err).Render(w, r)
		return
	}
	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, map[string]string{"status": "accepted"})
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
