package store

import (
	"context"
	"time"

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

// --- inventory sources ---

// InventorySource is an external source feeding an inventory.
type InventorySource struct {
	ID             int64      `json:"id" db:"id"`
	InventoryID    int64      `json:"inventory_id" db:"inventory_id"`
	Name           string     `json:"name" db:"name"`
	SourceKind     string     `json:"source_kind" db:"source_kind"`
	Source         string     `json:"source" db:"source"`
	CredentialID   *int64     `json:"credential_id" db:"credential_id"`
	UpdateOnLaunch bool       `json:"update_on_launch" db:"update_on_launch"`
	LastSyncedAt   *time.Time `json:"last_synced_at" db:"last_synced_at"`
}

// ListSources returns an inventory's sources, name-ordered.
func (s *InventoryStore) ListSources(ctx context.Context, inventoryID int64) ([]InventorySource, error) {
	sources := []InventorySource{}
	err := s.db.SelectContext(ctx, &sources,
		`SELECT id, inventory_id, name, source_kind, source, credential_id, update_on_launch, last_synced_at
		 FROM inventory_sources WHERE inventory_id = $1 ORDER BY name`, inventoryID)
	return sources, err
}

// CreateSource inserts an inventory source and returns its id.
func (s *InventoryStore) CreateSource(ctx context.Context, inventoryID int64, name, kind, source string, credentialID *int64, updateOnLaunch bool) (int64, error) {
	var id int64
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO inventory_sources (inventory_id, name, source_kind, source, credential_id, update_on_launch)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		inventoryID, name, kind, source, credentialID, updateOnLaunch).Scan(&id)
	return id, err
}

// DeleteSource removes an inventory source scoped to its inventory.
func (s *InventoryStore) DeleteSource(ctx context.Context, sourceID, inventoryID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM inventory_sources WHERE id = $1 AND inventory_id = $2`, sourceID, inventoryID)
	return err
}

// SourceName returns a source's name (scoped to its inventory).
func (s *InventoryStore) SourceName(ctx context.Context, sourceID, inventoryID int64) (string, error) {
	var name string
	err := s.db.GetContext(ctx, &name, `SELECT name FROM inventory_sources WHERE id = $1 AND inventory_id = $2`, sourceID, inventoryID)
	return name, err
}

// EnqueueSourceSync creates a pending unified_job for an inventory-source sync
// and returns its id.
func (s *InventoryStore) EnqueueSourceSync(ctx context.Context, jobName string, jobArgs []byte) (int64, error) {
	var jobID int64
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO unified_jobs (name, status, created_at, job_args)
		 VALUES ($1, 'pending', now(), $2) RETURNING id`, jobName, jobArgs).Scan(&jobID)
	return jobID, err
}

// --- inventory import (upsert host/group by name within an inventory) ---

// HostByName finds a host by name within an inventory.
func (s *InventoryStore) HostByName(ctx context.Context, inventoryID int64, name string) (models.Host, error) {
	var host models.Host
	err := s.db.GetContext(ctx, &host, `SELECT `+HostCols+` FROM hosts WHERE inventory_id = $1 AND name = $2`, inventoryID, name)
	return host, err
}

// CreateImportHost inserts a minimal enabled host during import.
func (s *InventoryStore) CreateImportHost(ctx context.Context, inventoryID int64, name string) (models.Host, error) {
	var host models.Host
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO hosts (inventory_id, name, enabled) VALUES ($1, $2, true) RETURNING `+HostCols,
		inventoryID, name).StructScan(&host)
	return host, err
}

// GroupByName finds a group by name within an inventory.
func (s *InventoryStore) GroupByName(ctx context.Context, inventoryID int64, name string) (models.Group, error) {
	var group models.Group
	err := s.db.GetContext(ctx, &group, `SELECT `+GroupCols+` FROM groups WHERE inventory_id = $1 AND name = $2`, inventoryID, name)
	return group, err
}

// CreateImportGroup inserts a minimal group during import.
func (s *InventoryStore) CreateImportGroup(ctx context.Context, inventoryID int64, name string) (models.Group, error) {
	var group models.Group
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO groups (inventory_id, name) VALUES ($1, $2) RETURNING `+GroupCols,
		inventoryID, name).StructScan(&group)
	return group, err
}

// LinkHostGroup adds a host to a group (idempotent).
func (s *InventoryStore) LinkHostGroup(ctx context.Context, hostID, groupID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO host_groups (host_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, hostID, groupID)
	return err
}
