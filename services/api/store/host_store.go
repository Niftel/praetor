package store

import (
	"context"
	"encoding/json"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
)

// HostStore is the data-access layer for the hosts domain (plus the host_facts
// and host_groups reads the host handlers need).
type HostStore struct {
	db *sqlx.DB
}

func NewHostStore(db *sqlx.DB) *HostStore { return &HostStore{db: db} }

// InventoryIDForHost returns the inventory a host belongs to (for RBAC via the
// parent inventory's roles). Returns an error if the host does not exist.
func (s *HostStore) InventoryIDForHost(ctx context.Context, hostID int64) (int64, error) {
	var invID int64
	err := s.db.GetContext(ctx, &invID, `SELECT inventory_id FROM hosts WHERE id = $1`, hostID)
	return invID, wrap("HostStore.InventoryIDForHost", err)
}

// Facts returns a host's cached ansible_facts (empty object when none).
func (s *HostStore) Facts(ctx context.Context, hostID int64) json.RawMessage {
	var facts json.RawMessage
	if err := s.db.GetContext(ctx, &facts, `SELECT facts FROM host_facts WHERE host_id = $1`, hostID); err != nil {
		return json.RawMessage("{}")
	}
	return facts
}

// ListByInventory returns an inventory's hosts, name-ordered.
func (s *HostStore) ListByInventory(ctx context.Context, inventoryID int64) ([]models.Host, error) {
	hosts := []models.Host{}
	err := s.db.SelectContext(ctx, &hosts, `SELECT `+HostCols+` FROM hosts WHERE inventory_id = $1 ORDER BY name`, inventoryID)
	return hosts, wrap("HostStore.ListByInventory", err)
}

// Get returns a single host by id.
func (s *HostStore) Get(ctx context.Context, id int64) (models.Host, error) {
	var host models.Host
	err := s.db.GetContext(ctx, &host, `SELECT `+HostCols+` FROM hosts WHERE id = $1`, id)
	return host, wrap("HostStore.Get", err)
}

// Create inserts a host (enabled defaulted true by the caller path) and returns it.
func (s *HostStore) Create(ctx context.Context, input models.Host) (models.Host, error) {
	query := `
		INSERT INTO hosts (inventory_id, name, description, variables, enabled, is_control_node)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + HostCols
	var created models.Host
	err := s.db.QueryRowxContext(ctx, query,
		input.InventoryID, input.Name, input.Description, input.Variables, true, input.IsControlNode,
	).StructScan(&created)
	return created, wrap("HostStore.Create", err)
}

// Update applies the merged host fields and returns the persisted row.
func (s *HostStore) Update(ctx context.Context, id int64, host models.Host) (models.Host, error) {
	query := `
		UPDATE hosts
		SET name = $2, description = $3, variables = $4, enabled = $5, is_control_node = $6, modified_at = now()
		WHERE id = $1
		RETURNING ` + HostCols
	var updated models.Host
	err := s.db.QueryRowxContext(ctx, query,
		id, host.Name, host.Description, host.Variables, host.Enabled, host.IsControlNode,
	).StructScan(&updated)
	return updated, wrap("HostStore.Update", err)
}

// Delete removes a host by id.
func (s *HostStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM hosts WHERE id = $1`, id)
	return wrap("HostStore.Delete", err)
}

// GroupsForHost returns the groups a host is a member of, name-ordered.
func (s *HostStore) GroupsForHost(ctx context.Context, hostID int64) ([]models.Group, error) {
	groups := []models.Group{}
	query := `
		SELECT ` + Prefixed("g", GroupCols) + ` FROM groups g
		JOIN host_groups hg ON g.id = hg.group_id
		WHERE hg.host_id = $1
		ORDER BY g.name`
	err := s.db.SelectContext(ctx, &groups, query, hostID)
	return groups, wrap("HostStore.GroupsForHost", err)
}

// SetRunner makes hostID the sole runner host of its inventory (clearing any
// previous one) in a single transaction, and returns the updated host.
func (s *HostStore) SetRunner(ctx context.Context, hostID int64) (models.Host, error) {
	var host models.Host
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return host, wrap("HostStore.SetRunner", err)
	}
	defer tx.Rollback()
	var inventoryID int64
	if err := tx.GetContext(ctx, &inventoryID, `SELECT inventory_id FROM hosts WHERE id = $1`, hostID); err != nil {
		return host, wrap("HostStore.SetRunner", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE hosts SET is_runner_host = FALSE WHERE inventory_id = $1`, inventoryID); err != nil {
		return host, wrap("HostStore.SetRunner", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE hosts SET is_runner_host = TRUE WHERE id = $1`, hostID); err != nil {
		return host, wrap("HostStore.SetRunner", err)
	}
	if err := tx.GetContext(ctx, &host, `SELECT `+HostCols+` FROM hosts WHERE id = $1`, hostID); err != nil {
		return host, wrap("HostStore.SetRunner", err)
	}
	return host, tx.Commit()
}

// RunnerHeartbeat stamps a runner host's health (no-op if it isn't a runner).
func (s *HostStore) RunnerHeartbeat(ctx context.Context, hostID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE hosts
		SET runner_last_seen = NOW(), runner_healthy = TRUE
		WHERE id = $1 AND is_runner_host = TRUE`, hostID)
	return wrap("HostStore.RunnerHeartbeat", err)
}
