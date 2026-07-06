package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
	"github.com/praetordev/praetor/services/api/store"
)

// InventoryStore is the inventories-domain data access the handler depends on.
type InventoryStore interface {
	ListAll(ctx context.Context, limit, offset int) ([]models.Inventory, error)
	CountAll(ctx context.Context) (int64, error)
	ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Inventory, error)
	Get(ctx context.Context, id int64) (models.Inventory, error)
	Create(ctx context.Context, input models.Inventory) (models.Inventory, error)
	UpdateContent(ctx context.Context, id int64, input models.Inventory) (models.Inventory, error)
	UpdateKind(ctx context.Context, id int64, input models.Inventory) (models.Inventory, error)
	Delete(ctx context.Context, id int64) error
}

// InventoriesResource handles inventory operations
type InventoriesResource struct {
	DB *sqlx.DB
	*Authorizer
	store InventoryStore
}

// NewInventoriesResource creates a new inventories resource handler
func NewInventoriesResource(db *sqlx.DB) *InventoriesResource {
	return &InventoriesResource{DB: db, Authorizer: NewAuthorizer(db), store: store.NewInventoryStore(db)}
}

// Routes creates a REST router for the Inventories resource
func (rs *InventoriesResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListInventories)
	r.Post("/", rs.CreateInventory)
	r.Get("/{id}", rs.GetInventory)
	r.Put("/{id}", rs.UpdateInventory)
	r.Delete("/{id}", rs.DeleteInventory)
	return r
}

// ListInventories GET /api/v1/inventories
func (rs *InventoriesResource) ListInventories(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)
	uc := currentUser(r)

	var inventories []models.Inventory
	var total int64

	if uc.IsSuperuser || uc.IsSystemAuditor {
		var err error
		if inventories, err = rs.store.ListAll(r.Context(), pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total, _ = rs.store.CountAll(r.Context())
	} else {
		ids, err := rs.readableIDs(r, rbac.ContentTypeInventory)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if inventories, err = rs.store.ListByIDs(r.Context(), ids, pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total = int64(len(ids))
	}

	if inventories == nil {
		inventories = []models.Inventory{}
	}

	render.JSON(w, r, &render.PaginatedResponse{
		Items:  inventories,
		Total:  total,
		Limit:  pg.Limit,
		Offset: pg.Offset,
	})
}

// CreateInventory POST /api/v1/inventories
func (rs *InventoriesResource) CreateInventory(w http.ResponseWriter, r *http.Request) {
	var input models.Inventory
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Validation
	if input.Name == "" {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	// An inventory must belong to an explicit organization (no silent org-1 default).
	if input.OrganizationID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r) // organization_id is required
		return
	}

	// Creating an inventory requires admin on its parent organization.
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, input.OrganizationID, actAdmin) {
		return
	}

	// Default kind
	if input.Kind == "" {
		input.Kind = "static"
	}

	created, err := rs.store.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	rs.grantCreatorAdmin(r.Context(), rbac.ContentTypeInventory, created.ID, currentUser(r))
	render.Created(w, r, created)
}

// GetInventory GET /api/v1/inventories/{id}
func (rs *InventoriesResource) GetInventory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, id, actRead) {
		return
	}

	inventory, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	render.JSON(w, r, inventory)
}

// UpdateInventory PUT /api/v1/inventories/{id}
func (rs *InventoriesResource) UpdateInventory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, id, actAdmin) {
		return
	}

	var input models.Inventory
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	updated, err := rs.store.UpdateContent(r.Context(), id, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, updated)
}

// DeleteInventory DELETE /api/v1/inventories/{id}
func (rs *InventoriesResource) DeleteInventory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, id, actAdmin) {
		return
	}

	if err := rs.store.Delete(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetInventoryByParam GET /api/v1/inventories/{inventoryId} - uses inventoryId param
func (rs *InventoriesResource) GetInventoryByParam(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "inventoryId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, id, actRead) {
		return
	}

	inventory, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	render.JSON(w, r, inventory)
}

// UpdateInventoryByParam PUT /api/v1/inventories/{inventoryId} - uses inventoryId param
func (rs *InventoriesResource) UpdateInventoryByParam(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "inventoryId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, id, actAdmin) {
		return
	}

	var input models.Inventory
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	updated, err := rs.store.UpdateKind(r.Context(), id, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, updated)
}

// DeleteInventoryByParam DELETE /api/v1/inventories/{inventoryId} - uses inventoryId param
func (rs *InventoriesResource) DeleteInventoryByParam(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "inventoryId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.ContentTypeInventory, id, actAdmin) {
		return
	}

	if err := rs.store.Delete(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
