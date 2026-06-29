package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

// InventoriesResource handles inventory operations
type InventoriesResource struct {
	DB *sqlx.DB
	*Authorizer
}

// NewInventoriesResource creates a new inventories resource handler
func NewInventoriesResource(db *sqlx.DB) *InventoriesResource {
	return &InventoriesResource{DB: db, Authorizer: NewAuthorizer(db)}
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
		if err := rs.DB.SelectContext(r.Context(), &inventories, `SELECT * FROM inventories ORDER BY id DESC LIMIT $1 OFFSET $2`, pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		_ = rs.DB.Get(&total, "SELECT count(*) FROM inventories")
	} else {
		ids, err := rs.readableIDs(r, rbac.ContentTypeInventory)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if len(ids) > 0 {
			q, args, _ := sqlx.In(`SELECT * FROM inventories WHERE id IN (?) ORDER BY id DESC LIMIT ? OFFSET ?`, ids, pg.Limit, pg.Offset)
			q = rs.DB.Rebind(q)
			if err := rs.DB.SelectContext(r.Context(), &inventories, q, args...); err != nil {
				render.ErrInternal(err).Render(w, r)
				return
			}
			total = int64(len(ids))
		}
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

	// Default organization to 1 if not set
	if input.OrganizationID == 0 {
		input.OrganizationID = 1
	}

	// Creating an inventory requires admin on its parent organization.
	if !rs.authorize(w, r, rbac.ContentTypeOrganization, input.OrganizationID, actAdmin) {
		return
	}

	// Default kind
	if input.Kind == "" {
		input.Kind = "static"
	}

	query := `
		INSERT INTO inventories (organization_id, name, description, kind, content) 
		VALUES ($1, $2, $3, $4, $5) 
		RETURNING *`

	var created models.Inventory
	err := rs.DB.QueryRowxContext(r.Context(), query,
		input.OrganizationID, input.Name, input.Description,
		input.Kind, input.Content,
	).StructScan(&created)

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

	var inventory models.Inventory
	query := `SELECT * FROM inventories WHERE id = $1`
	err = rs.DB.GetContext(r.Context(), &inventory, query, id)
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

	query := `
		UPDATE inventories
		SET name = $2, description = $3, content = $4, modified_at = now()
		WHERE id = $1 
		RETURNING *`

	var updated models.Inventory
	err = rs.DB.QueryRowxContext(r.Context(), query,
		id, input.Name, input.Description, input.Content,
	).StructScan(&updated)

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

	query := `DELETE FROM inventories WHERE id = $1`
	_, err = rs.DB.ExecContext(r.Context(), query, id)
	if err != nil {
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

	var inventory models.Inventory
	query := `SELECT * FROM inventories WHERE id = $1`
	err = rs.DB.GetContext(r.Context(), &inventory, query, id)
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

	query := `
		UPDATE inventories 
		SET name = $2, description = $3, kind = $4, modified_at = now() 
		WHERE id = $1 
		RETURNING *`

	var updated models.Inventory
	err = rs.DB.QueryRowxContext(r.Context(), query,
		id, input.Name, input.Description, input.Kind,
	).StructScan(&updated)

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

	query := `DELETE FROM inventories WHERE id = $1`
	_, err = rs.DB.ExecContext(r.Context(), query, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
