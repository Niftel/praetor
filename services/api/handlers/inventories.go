package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/launch"
	"github.com/praetordev/models"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
)

// InventoryStore is the inventories-domain data access the handler depends on.
type InventoryStore interface {
	ListAll(ctx context.Context, limit, offset int) ([]models.Inventory, error)
	CountAll(ctx context.Context) (int64, error)
	ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Inventory, error)
	Get(ctx context.Context, id int64) (models.Inventory, error)
	Create(ctx context.Context, input models.Inventory) (models.Inventory, error)
	UpdateKind(ctx context.Context, id int64, input models.Inventory) (models.Inventory, error)
	Delete(ctx context.Context, id int64) error
	// inventory sources
	ListSources(ctx context.Context, inventoryID int64) ([]store.InventorySource, error)
	CreateSource(ctx context.Context, inventoryID int64, name, kind, source string, credentialID *int64, updateOnLaunch bool) (int64, error)
	DeleteSource(ctx context.Context, sourceID, inventoryID int64) error
	SourceName(ctx context.Context, sourceID, inventoryID int64) (string, error)
	EnqueueSourceSync(ctx context.Context, jobName string, opts launch.Options) (int64, error)
	// inventory import
	HostByName(ctx context.Context, inventoryID int64, name string) (models.Host, error)
	CreateImportHost(ctx context.Context, inventoryID int64, name string) (models.Host, error)
	GroupByName(ctx context.Context, inventoryID int64, name string) (models.Group, error)
	CreateImportGroup(ctx context.Context, inventoryID int64, name string) (models.Group, error)
	LinkHostGroup(ctx context.Context, hostID, groupID int64) error
}

// InventoriesResource handles inventory operations
type InventoriesResource struct {
	DB *sqlx.DB
	*Authorizer
	store InventoryStore
}

// NewInventoriesResource creates a new inventories resource handler
func NewInventoriesResource(db *sqlx.DB, authz *Authorizer) *InventoriesResource {
	return &InventoriesResource{DB: db, Authorizer: authz, store: store.NewInventoryStore(db)}
}

// ListInventories GET /api/v1/inventories
func (rs *InventoriesResource) ListInventories(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)

	var inventories []models.Inventory
	var total int64

	viewAll, verr := rs.canViewAll(r, rbac.Inventory)
	if verr != nil {
		render.ErrInternal(verr).Render(w, r)
		return
	}
	if viewAll {
		var err error
		if inventories, err = rs.store.ListAll(r.Context(), pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total, _ = rs.store.CountAll(r.Context())
	} else {
		ids, err := rs.readableIDs(r, rbac.Inventory)
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
		Items:  dto.FromInventories(inventories),
		Total:  total,
		Limit:  pg.Limit,
		Offset: pg.Offset,
	})
}

// CreateInventory POST /api/v1/inventories
func (rs *InventoriesResource) CreateInventory(w http.ResponseWriter, r *http.Request) {
	var body dto.Inventory
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	input := body.ToModel()

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

	// Creating an inventory requires the org's inventory_admin_role (org admins
	// and superusers inherit it through the role hierarchy).
	if !rs.authorizeOrgRole(w, r, input.OrganizationID, rbac.InventoryAdminRole) {
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

	rs.grantCreatorAdmin(r.Context(), rbac.Inventory, created.ID, currentUser(r))
	render.Created(w, r, dto.FromInventory(created))
}

// GetInventoryByParam GET /api/v1/inventories/{inventoryId} - uses inventoryId param
func (rs *InventoriesResource) GetInventoryByParam(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "inventoryId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.Inventory, id, actRead) {
		return
	}

	inventory, err := rs.store.Get(r.Context(), id)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	render.JSON(w, r, dto.FromInventory(inventory))
}

// UpdateInventoryByParam PUT /api/v1/inventories/{inventoryId} - uses inventoryId param
func (rs *InventoriesResource) UpdateInventoryByParam(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "inventoryId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.Inventory, id, actAdmin) {
		return
	}

	var body dto.Inventory
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	updated, err := rs.store.UpdateKind(r.Context(), id, body.ToModel())
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	render.JSON(w, r, dto.FromInventory(updated))
}

// DeleteInventoryByParam DELETE /api/v1/inventories/{inventoryId} - uses inventoryId param
func (rs *InventoriesResource) DeleteInventoryByParam(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "inventoryId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	if !rs.authorize(w, r, rbac.Inventory, id, actAdmin) {
		return
	}

	if err := rs.store.Delete(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
