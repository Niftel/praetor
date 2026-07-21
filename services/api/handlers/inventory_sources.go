package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/launch"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/pkg/inventorysourcecatalog"
	"github.com/praetordev/render"
)

type InventorySyncHistory struct {
	ID                   int64           `db:"id" json:"id"`
	CorrelationID        string          `db:"correlation_id" json:"correlation_id"`
	InventoryID          *int64          `db:"inventory_id" json:"inventory_id,omitempty"`
	InventorySourceID    *int64          `db:"inventory_source_id" json:"inventory_source_id,omitempty"`
	UnifiedJobID         *int64          `db:"unified_job_id" json:"unified_job_id,omitempty"`
	ExecutionRunID       *string         `db:"execution_run_id" json:"execution_run_id,omitempty"`
	CredentialID         *int64          `db:"credential_id" json:"credential_id,omitempty"`
	ReconciliationPolicy string          `db:"reconciliation_policy" json:"reconciliation_policy"`
	Phase                string          `db:"phase" json:"phase"`
	Status               string          `db:"status" json:"status"`
	HostsAdded           int             `db:"hosts_added" json:"hosts_added"`
	HostsUpdated         int             `db:"hosts_updated" json:"hosts_updated"`
	HostsDisabled        int             `db:"hosts_disabled" json:"hosts_disabled"`
	HostsUnchanged       int             `db:"hosts_unchanged" json:"hosts_unchanged"`
	GroupsAdded          int             `db:"groups_added" json:"groups_added"`
	GroupsUpdated        int             `db:"groups_updated" json:"groups_updated"`
	GroupsUnchanged      int             `db:"groups_unchanged" json:"groups_unchanged"`
	DiagnosticCode       *string         `db:"diagnostic_code" json:"diagnostic_code,omitempty"`
	DiagnosticMessage    *string         `db:"diagnostic_message" json:"diagnostic_message,omitempty"`
	DiagnosticDetails    json.RawMessage `db:"diagnostic_details" json:"diagnostic_details"`
	StartedAt            *time.Time      `db:"started_at" json:"started_at,omitempty"`
	FinishedAt           *time.Time      `db:"finished_at" json:"finished_at,omitempty"`
	CreatedAt            time.Time       `db:"created_at" json:"created_at"`
}

type InventorySyncHistoryResponse struct {
	Results []InventorySyncHistory `json:"results"`
	Total   int                    `json:"total"`
}

type inventorySyncHistoryFilter struct {
	Status string
	Phase  string
	Limit  int
}

func parseInventorySyncHistoryFilter(r *http.Request) (inventorySyncHistoryFilter, error) {
	filter := inventorySyncHistoryFilter{Status: r.URL.Query().Get("status"), Phase: r.URL.Query().Get("phase"), Limit: 25}
	if filter.Status != "" && filter.Status != "pending" && filter.Status != "running" && filter.Status != "successful" && filter.Status != "failed" && filter.Status != "canceled" {
		return filter, fmt.Errorf("status: unsupported inventory sync status %q", filter.Status)
	}
	if filter.Phase != "" && filter.Phase != "queued" && filter.Phase != "acquisition" && filter.Phase != "parsing" && filter.Phase != "validation" && filter.Phase != "reconciliation" && filter.Phase != "completed" {
		return filter, fmt.Errorf("phase: unsupported inventory sync phase %q", filter.Phase)
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		requested, err := strconv.Atoi(raw)
		if err != nil || requested <= 0 {
			return filter, fmt.Errorf("limit: must be a positive integer")
		}
		if requested > 100 {
			requested = 100
		}
		filter.Limit = requested
	}
	return filter, nil
}

var sensitiveDiagnosticKeys = map[string]struct{}{
	"authorization": {}, "cookie": {}, "credential": {}, "credentials": {},
	"password": {}, "private_key": {}, "secret": {}, "secret_key": {},
	"token": {}, "access_token": {}, "refresh_token": {}, "api_key": {},
}

func redactDiagnosticValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			if _, sensitive := sensitiveDiagnosticKeys[normalized]; sensitive {
				typed[key] = "[REDACTED]"
				continue
			}
			typed[key] = redactDiagnosticValue(nested)
		}
	case []any:
		for i := range typed {
			typed[i] = redactDiagnosticValue(typed[i])
		}
	}
	return value
}

func redactDiagnosticDetails(raw json.RawMessage) json.RawMessage {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(`{}`)
	}
	redacted, err := json.Marshal(redactDiagnosticValue(value))
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return redacted
}

func inventoryIDParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "inventoryId"), 10, 64)
}

// ListInventorySources GET /api/v1/inventories/{inventoryId}/sources
func (rs *InventoriesResource) ListInventorySources(w http.ResponseWriter, r *http.Request) {
	invID, err := inventoryIDParam(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.Inventory, invID, actRead) {
		return
	}
	sources, err := rs.store.ListSources(r.Context(), invID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, sources)
}

// ListInventorySourceHistory GET /api/v1/inventories/{inventoryId}/sources/{sourceId}/history
// Returns bounded, newest-first sync history. Authorization follows the parent
// inventory, so organization membership alone never exposes another team's
// source diagnostics. Stored provider details are defensively redacted again at
// the API boundary.
func (rs *InventoriesResource) ListInventorySourceHistory(w http.ResponseWriter, r *http.Request) {
	invID, err := inventoryIDParam(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.Inventory, invID, actRead) {
		return
	}
	sourceID, err := strconv.ParseInt(chi.URLParam(r, "sourceId"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	var exists bool
	if err := rs.DB.GetContext(r.Context(), &exists,
		`SELECT EXISTS(SELECT 1 FROM inventory_sources WHERE id=$1 AND inventory_id=$2)`, sourceID, invID); err != nil || !exists {
		if err != nil && err != sql.ErrNoRows {
			render.ErrInternal(err).Render(w, r)
		} else {
			render.ErrNotFound(nil).Render(w, r)
		}
		return
	}

	filter, err := parseInventorySyncHistoryFilter(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	rows := []InventorySyncHistory{}
	err = rs.DB.SelectContext(r.Context(), &rows, `
		SELECT id, correlation_id::text, inventory_id, inventory_source_id,
		       unified_job_id, execution_run_id::text, credential_id,
		       reconciliation_policy, phase, status,
		       hosts_added, hosts_updated, hosts_disabled, hosts_unchanged,
		       groups_added, groups_updated, groups_unchanged,
		       diagnostic_code, diagnostic_message, diagnostic_details,
		       started_at, finished_at, created_at
		  FROM inventory_sync_history
		 WHERE inventory_id=$1 AND inventory_source_id=$2
		   AND ($3='' OR status=$3) AND ($4='' OR phase=$4)
		 ORDER BY created_at DESC, id DESC LIMIT $5`, invID, sourceID, filter.Status, filter.Phase, filter.Limit)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	for i := range rows {
		rows[i].DiagnosticDetails = redactDiagnosticDetails(rows[i].DiagnosticDetails)
	}
	render.JSON(w, r, InventorySyncHistoryResponse{Results: rows, Total: len(rows)})
}

// CreateInventorySource POST /api/v1/inventories/{inventoryId}/sources
func (rs *InventoriesResource) CreateInventorySource(w http.ResponseWriter, r *http.Request) {
	invID, err := inventoryIDParam(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.Inventory, invID, actAdmin) {
		return
	}
	var body struct {
		Name                 string `json:"name"`
		SourceType           string `json:"source_type"`
		SourceKind           string `json:"source_kind"`
		Source               string `json:"source"`
		CredentialID         *int64 `json:"credential_id"`
		UpdateOnLaunch       bool   `json:"update_on_launch"`
		ReconciliationPolicy string `json:"reconciliation_policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if body.SourceKind == "" {
		body.SourceKind = "inventory"
	}
	if body.ReconciliationPolicy == "" {
		body.ReconciliationPolicy = "disable_missing"
	}
	if body.ReconciliationPolicy != "disable_missing" && body.ReconciliationPolicy != "retain_missing" {
		render.ErrInvalidRequest(fmt.Errorf("reconciliation_policy: must be disable_missing or retain_missing")).Render(w, r)
		return
	}
	if body.SourceType != "" {
		sourceType, ok := inventorysourcecatalog.Get(body.SourceType)
		if !ok {
			render.ErrInvalidRequest(fmt.Errorf("source_type: unsupported inventory source type %q", body.SourceType)).Render(w, r)
			return
		}
		if body.SourceKind == "script" {
			if !sourceType.Advanced {
				render.ErrInvalidRequest(fmt.Errorf("source_kind: scripts require the custom advanced source type")).Render(w, r)
				return
			}
		} else if fieldErrors := inventorysourcecatalog.Validate(body.SourceType, body.Source); len(fieldErrors) > 0 {
			render.ErrInvalidRequest(fieldErrors[0]).Render(w, r)
			return
		}
		if body.CredentialID != nil && len(sourceType.CompatibleCredentialTypes) > 0 {
			var credentialType string
			err := rs.DB.GetContext(r.Context(), &credentialType, `
				SELECT ct.name FROM credentials c
				JOIN credential_types ct ON ct.id = c.credential_type_id
				WHERE c.id = $1`, *body.CredentialID)
			if err != nil {
				render.ErrInvalidRequest(fmt.Errorf("credential_id: unknown credential")).Render(w, r)
				return
			}
			if !inventorysourcecatalog.SupportsCredentialType(sourceType, credentialType) {
				render.ErrInvalidRequest(fmt.Errorf("credential_id: credential type %q is not compatible with %q", credentialType, body.SourceType)).Render(w, r)
				return
			}
		}
	}
	// Attaching a credential requires use access to it (AWX attach semantics).
	if body.CredentialID != nil && !rs.authorize(w, r, rbac.Credential, *body.CredentialID, actUse) {
		return
	}
	id, err := rs.store.CreateSource(r.Context(), invID, body.Name, body.SourceKind, body.Source, body.CredentialID, body.UpdateOnLaunch)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if _, err := rs.DB.ExecContext(r.Context(),
		`UPDATE inventory_sources SET reconciliation_policy=$1 WHERE id=$2 AND inventory_id=$3`,
		body.ReconciliationPolicy, id, invID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"id": id})
}

// DeleteInventorySource DELETE /api/v1/inventories/{inventoryId}/sources/{sourceId}
func (rs *InventoriesResource) DeleteInventorySource(w http.ResponseWriter, r *http.Request) {
	invID, err := inventoryIDParam(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.Inventory, invID, actAdmin) {
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sourceId"), 10, 64)
	if err := rs.store.DeleteSource(r.Context(), sid, invID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SyncInventorySource POST /api/v1/inventories/{inventoryId}/sources/{sourceId}/sync
// Enqueues a sync job; the scheduler turns it into an executor inventory-sync run.
func (rs *InventoriesResource) SyncInventorySource(w http.ResponseWriter, r *http.Request) {
	rs.enqueueInventorySourceOperation(w, r, false)
}

// PreviewInventorySource POST /api/v1/inventories/{inventoryId}/sources/{sourceId}/preview
// Uses the same isolated execution and credential resolution as sync, but the
// executor is contractually forbidden from posting inventory data to ingestion.
func (rs *InventoriesResource) PreviewInventorySource(w http.ResponseWriter, r *http.Request) {
	rs.enqueueInventorySourceOperation(w, r, true)
}

func (rs *InventoriesResource) enqueueInventorySourceOperation(w http.ResponseWriter, r *http.Request, preview bool) {
	invID, err := inventoryIDParam(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	// Running a source sync is the AWX update_role action — no admin required.
	// Inventory admins inherit update_role; defining/deleting sources still needs
	// admin (handled on those endpoints).
	if !rs.authorize(w, r, rbac.Inventory, invID, actUpdate) {
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sourceId"), 10, 64)

	name, err := rs.store.SourceName(r.Context(), sid, invID)
	if err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if !preview {
		var active bool
		if err := rs.DB.GetContext(r.Context(), &active, `SELECT EXISTS(
			SELECT 1 FROM inventory_sync_history
			WHERE inventory_source_id=$1 AND status IN ('pending','running'))`, sid); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if active {
			render.Render(w, r, ErrConflict(fmt.Errorf("inventory source synchronization already active")))
			return
		}
	}

	opts := launch.Options{InventorySourceID: sid, InventoryPreview: preview}
	operation := "Inventory sync: "
	if preview {
		operation = "Inventory preview: "
	}
	jobID, err := rs.store.EnqueueSourceSync(r.Context(), operation+name, opts)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"job_id": jobID, "status": "pending", "preview": preview})
}
