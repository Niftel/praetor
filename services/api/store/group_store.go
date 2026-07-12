package store

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
)

// GroupStore is the data-access layer for the groups domain (incl. host_groups
// membership).
type GroupStore struct {
	db *sqlx.DB
}

func NewGroupStore(db *sqlx.DB) *GroupStore { return &GroupStore{db: db} }

// InventoryIDForGroup returns the inventory a group belongs to (for RBAC via the
// parent inventory's roles). Returns an error if the group does not exist.
func (s *GroupStore) InventoryIDForGroup(ctx context.Context, groupID int64) (int64, error) {
	var invID int64
	err := s.db.GetContext(ctx, &invID, `SELECT inventory_id FROM groups WHERE id = $1`, groupID)
	return invID, wrap("GroupStore.InventoryIDForGroup", err)
}

// ListByInventory returns an inventory's groups, name-ordered.
func (s *GroupStore) ListByInventory(ctx context.Context, inventoryID int64) ([]models.Group, error) {
	groups := []models.Group{}
	err := s.db.SelectContext(ctx, &groups, `SELECT `+GroupCols+` FROM groups WHERE inventory_id = $1 ORDER BY name`, inventoryID)
	return groups, wrap("GroupStore.ListByInventory", err)
}

// Get returns a single group by id.
func (s *GroupStore) Get(ctx context.Context, id int64) (models.Group, error) {
	var group models.Group
	err := s.db.GetContext(ctx, &group, `SELECT `+GroupCols+` FROM groups WHERE id = $1`, id)
	return group, wrap("GroupStore.Get", err)
}

// Create inserts a group and returns the persisted row.
func (s *GroupStore) Create(ctx context.Context, input models.Group) (models.Group, error) {
	query := `
		INSERT INTO groups (inventory_id, name, description, variables)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + GroupCols
	var created models.Group
	err := s.db.QueryRowxContext(ctx, query,
		input.InventoryID, input.Name, input.Description, input.Variables,
	).StructScan(&created)
	return created, wrap("GroupStore.Create", err)
}

// Update applies an edit to a group and returns the persisted row.
func (s *GroupStore) Update(ctx context.Context, id int64, input models.Group) (models.Group, error) {
	query := `
		UPDATE groups
		SET name = $2, description = $3, variables = $4, modified_at = now()
		WHERE id = $1
		RETURNING ` + GroupCols
	var updated models.Group
	err := s.db.QueryRowxContext(ctx, query, id, input.Name, input.Description, input.Variables).StructScan(&updated)
	return updated, wrap("GroupStore.Update", err)
}

// Delete removes a group by id.
func (s *GroupStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id = $1`, id)
	return wrap("GroupStore.Delete", err)
}

// AddHost adds a host to a group (idempotent).
func (s *GroupStore) AddHost(ctx context.Context, groupID, hostID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO host_groups (host_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, hostID, groupID)
	return wrap("GroupStore.AddHost", err)
}

// RemoveHost removes a host from a group.
func (s *GroupStore) RemoveHost(ctx context.Context, groupID, hostID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM host_groups WHERE host_id = $1 AND group_id = $2`, hostID, groupID)
	return wrap("GroupStore.RemoveHost", err)
}

// HostsInGroup returns the hosts that are members of a group, name-ordered.
func (s *GroupStore) HostsInGroup(ctx context.Context, groupID int64) ([]models.Host, error) {
	hosts := []models.Host{}
	query := `
		SELECT ` + Prefixed("h", HostCols) + ` FROM hosts h
		JOIN host_groups hg ON h.id = hg.host_id
		WHERE hg.group_id = $1
		ORDER BY h.name`
	err := s.db.SelectContext(ctx, &hosts, query, groupID)
	return hosts, wrap("GroupStore.HostsInGroup", err)
}
