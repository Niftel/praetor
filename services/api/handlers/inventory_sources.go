package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

type inventorySource struct {
	ID             int64      `json:"id" db:"id"`
	InventoryID    int64      `json:"inventory_id" db:"inventory_id"`
	Name           string     `json:"name" db:"name"`
	SourceKind     string     `json:"source_kind" db:"source_kind"`
	Source         string     `json:"source" db:"source"`
	CredentialID   *int64     `json:"credential_id" db:"credential_id"`
	UpdateOnLaunch bool       `json:"update_on_launch" db:"update_on_launch"`
	LastSyncedAt   *time.Time `json:"last_synced_at" db:"last_synced_at"`
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
	if !rs.authorize(w, r, rbac.ContentTypeInventory, invID, actRead) {
		return
	}
	sources := []inventorySource{}
	if err := rs.DB.SelectContext(r.Context(), &sources,
		`SELECT id, inventory_id, name, source_kind, source, credential_id, update_on_launch, last_synced_at
		 FROM inventory_sources WHERE inventory_id = $1 ORDER BY name`, invID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, sources)
}

// CreateInventorySource POST /api/v1/inventories/{inventoryId}/sources
func (rs *InventoriesResource) CreateInventorySource(w http.ResponseWriter, r *http.Request) {
	invID, err := inventoryIDParam(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeInventory, invID, actAdmin) {
		return
	}
	var body struct {
		Name           string `json:"name"`
		SourceKind     string `json:"source_kind"`
		Source         string `json:"source"`
		CredentialID   *int64 `json:"credential_id"`
		UpdateOnLaunch bool   `json:"update_on_launch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}
	if body.SourceKind == "" {
		body.SourceKind = "inventory"
	}
	// Attaching a credential requires use access to it (AWX attach semantics).
	if body.CredentialID != nil && !rs.authorize(w, r, rbac.ContentTypeCredential, *body.CredentialID, actUse) {
		return
	}
	var id int64
	if err := rs.DB.QueryRowxContext(r.Context(),
		`INSERT INTO inventory_sources (inventory_id, name, source_kind, source, credential_id, update_on_launch)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		invID, body.Name, body.SourceKind, body.Source, body.CredentialID, body.UpdateOnLaunch).Scan(&id); err != nil {
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
	if !rs.authorize(w, r, rbac.ContentTypeInventory, invID, actAdmin) {
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sourceId"), 10, 64)
	if _, err := rs.DB.ExecContext(r.Context(),
		`DELETE FROM inventory_sources WHERE id = $1 AND inventory_id = $2`, sid, invID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SyncInventorySource POST /api/v1/inventories/{inventoryId}/sources/{sourceId}/sync
// Enqueues a sync job; the scheduler turns it into an executor inventory-sync run.
func (rs *InventoriesResource) SyncInventorySource(w http.ResponseWriter, r *http.Request) {
	invID, err := inventoryIDParam(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.ContentTypeInventory, invID, actAdmin) {
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sourceId"), 10, 64)

	var name string
	if err := rs.DB.GetContext(r.Context(), &name,
		`SELECT name FROM inventory_sources WHERE id = $1 AND inventory_id = $2`, sid, invID); err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	jobArgs, _ := json.Marshal(map[string]interface{}{"inventory_source_id": sid})
	var jobID int64
	if err := rs.DB.QueryRowxContext(r.Context(),
		`INSERT INTO unified_jobs (name, status, created_at, job_args)
		 VALUES ($1, 'pending', now(), $2) RETURNING id`,
		"Inventory sync: "+name, jobArgs).Scan(&jobID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"job_id": jobID, "status": "pending"})
}
