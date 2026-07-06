package store

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
)

// InventoryStore is the data-access layer for the inventories domain.
type InventoryStore struct {
	db *sqlx.DB
}

func NewInventoryStore(db *sqlx.DB) *InventoryStore { return &InventoryStore{db: db} }

// ListAll returns a page of all inventories (superuser/auditor view).
func (s *InventoryStore) ListAll(ctx context.Context, limit, offset int) ([]models.Inventory, error) {
	inventories := []models.Inventory{}
	err := s.db.SelectContext(ctx, &inventories,
		`SELECT `+InventoryCols+` FROM inventories ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	return inventories, err
}

// CountAll returns the total number of inventories.
func (s *InventoryStore) CountAll(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.GetContext(ctx, &total, "SELECT count(*) FROM inventories")
	return total, err
}

// ListByIDs returns a page of the inventories whose id is in ids.
func (s *InventoryStore) ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Inventory, error) {
	inventories := []models.Inventory{}
	if len(ids) == 0 {
		return inventories, nil
	}
	q, args, err := sqlx.In(`SELECT `+InventoryCols+` FROM inventories WHERE id IN (?) ORDER BY id DESC LIMIT ? OFFSET ?`, ids, limit, offset)
	if err != nil {
		return nil, err
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &inventories, q, args...)
	return inventories, err
}

// Get returns a single inventory by id.
func (s *InventoryStore) Get(ctx context.Context, id int64) (models.Inventory, error) {
	var inv models.Inventory
	err := s.db.GetContext(ctx, &inv, `SELECT `+InventoryCols+` FROM inventories WHERE id = $1`, id)
	return inv, err
}

// Create inserts an inventory and returns the persisted row.
func (s *InventoryStore) Create(ctx context.Context, input models.Inventory) (models.Inventory, error) {
	query := `
		INSERT INTO inventories (organization_id, name, description, kind, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + InventoryCols
	var created models.Inventory
	err := s.db.QueryRowxContext(ctx, query,
		input.OrganizationID, input.Name, input.Description, input.Kind, input.Content,
	).StructScan(&created)
	return created, err
}

// UpdateContent updates name/description/content (the /{id} edit path).
func (s *InventoryStore) UpdateContent(ctx context.Context, id int64, input models.Inventory) (models.Inventory, error) {
	query := `
		UPDATE inventories
		SET name = $2, description = $3, content = $4, modified_at = now()
		WHERE id = $1
		RETURNING ` + InventoryCols
	var updated models.Inventory
	err := s.db.QueryRowxContext(ctx, query, id, input.Name, input.Description, input.Content).StructScan(&updated)
	return updated, err
}

// UpdateKind updates name/description/kind (the /{inventoryId} edit path).
func (s *InventoryStore) UpdateKind(ctx context.Context, id int64, input models.Inventory) (models.Inventory, error) {
	query := `
		UPDATE inventories
		SET name = $2, description = $3, kind = $4, modified_at = now()
		WHERE id = $1
		RETURNING ` + InventoryCols
	var updated models.Inventory
	err := s.db.QueryRowxContext(ctx, query, id, input.Name, input.Description, input.Kind).StructScan(&updated)
	return updated, err
}

// Delete removes an inventory by id.
func (s *InventoryStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM inventories WHERE id = $1`, id)
	return err
}
