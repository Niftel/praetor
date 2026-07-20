package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/store"
)

var (
	diagnosticStreamPollInterval = 750 * time.Millisecond
	diagnosticStreamHeartbeat    = 10 * time.Second
)

func (rs *JobsResource) canStreamRun(r *http.Request, runID uuid.UUID) (bool, error) {
	templateID, governed, err := rs.store.TemplateIDForRun(r.Context(), runID)
	if err != nil {
		return false, err
	}
	if governed {
		allowed, err := rs.canAuthorize(r, rbac.JobTemplate, templateID, actRead)
		if err != nil || !allowed {
			return allowed, err
		}
	} else {
		allowed, err := rs.canViewAll(r, rbac.JobTemplate)
		if err != nil || !allowed {
			return allowed, err
		}
	}
	inventoryID, err := rs.store.InventoryIDForRun(r.Context(), runID)
	if err != nil {
		return false, err
	}
	if inventoryID != nil {
		return rs.canAuthorize(r, rbac.Inventory, *inventoryID, actRead)
	}
	return true, nil
}

// StreamRunDiagnostics provides a resumable SSE stream over the exact same
// redacted event DTO as the paginated diagnostics endpoint. The event sequence
// is the SSE id and the exclusive resume cursor, so reconnects neither skip nor
// duplicate an acknowledged event.
func (rs *JobsResource) StreamRunDiagnostics(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}
	cursor, err := diagnosticStreamCursor(r)
	if err != nil {
		render.Render(w, r, ErrInvalidRequest(err))
		return
	}
	allowed, err := rs.canStreamRun(r, runID)
	if err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	if !allowed {
		render.Render(w, r, ErrForbidden)
		return
	}
	// Resolve the run before committing stream headers so unknown run IDs retain
	// the diagnostics endpoint's normal not-found/error response semantics.
	if _, err := rs.store.DiagnosticSummary(r.Context(), runID); err != nil {
		render.Render(w, r, ErrInternal(err))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		render.Render(w, r, ErrInternal(fmt.Errorf("streaming unsupported")))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "retry: 2000\n\n")
	flusher.Flush()

	ticker := time.NewTicker(diagnosticStreamPollInterval)
	defer ticker.Stop()
	lastHeartbeat := time.Now()
	for {
		// Re-evaluate both template and inventory grants for every page. Revocation
		// closes an already-open connection without leaking another event.
		allowed, err = rs.canStreamRun(r, runID)
		if err != nil || !allowed {
			return
		}
		page, err := rs.store.ListDiagnostics(r.Context(), runID, store.DiagnosticQuery{AfterSeq: cursor, Limit: 100, Kind: "all"})
		if err != nil {
			return
		}
		if len(page) > 100 {
			page = page[:100]
		}
		for _, event := range page {
			payload, err := json.Marshal(dto.FromDiagnosticEvent(event))
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: diagnostic\ndata: %s\n\n", event.Seq, payload); err != nil {
				return
			}
			cursor = event.Seq
		}
		if len(page) > 0 {
			flusher.Flush()
		}

		summary, err := rs.store.DiagnosticSummary(r.Context(), runID)
		if err != nil {
			return
		}
		if diagnosticTerminal(summary.RunState) && cursor >= summary.LastEventSeq {
			payload, _ := json.Marshal(map[string]interface{}{"state": summary.RunState, "cursor": cursor})
			_, _ = fmt.Fprintf(w, "event: terminal\ndata: %s\n\n", payload)
			flusher.Flush()
			return
		}
		if time.Since(lastHeartbeat) >= diagnosticStreamHeartbeat {
			if _, err := fmt.Fprintf(w, ": heartbeat %d\n\n", cursor); err != nil {
				return
			}
			flusher.Flush()
			lastHeartbeat = time.Now()
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func diagnosticStreamCursor(r *http.Request) (int64, error) {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("cursor")
	}
	if raw == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cursor < 0 {
		return 0, errors.New("diagnostic cursor must be a non-negative event sequence")
	}
	return cursor, nil
}

func diagnosticTerminal(state string) bool {
	switch state {
	case "successful", "failed", "canceled", "error", "lost":
		return true
	default:
		return false
	}
}
