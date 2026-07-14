package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/launch"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/render"
)

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
	sources, err := rs.store.ListSources(r.Context(), invID)
	if err != nil {
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
	id, err := rs.store.CreateSource(r.Context(), invID, body.Name, body.SourceKind, body.Source, body.CredentialID, body.UpdateOnLaunch)
	if err != nil {
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
	if err := rs.store.DeleteSource(r.Context(), sid, invID); err != nil {
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
	// Running a source sync is the AWX update_role action — no admin required.
	// Inventory admins inherit update_role; defining/deleting sources still needs
	// admin (handled on those endpoints).
	if !rs.authorize(w, r, rbac.ContentTypeInventory, invID, actUpdate) {
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sourceId"), 10, 64)

	name, err := rs.store.SourceName(r.Context(), sid, invID)
	if err != nil {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	opts := launch.Options{InventorySourceID: sid}
	jobID, err := rs.store.EnqueueSourceSync(r.Context(), "Inventory sync: "+name, opts)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{"job_id": jobID, "status": "pending"})
}
