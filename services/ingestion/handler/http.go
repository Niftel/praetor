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
