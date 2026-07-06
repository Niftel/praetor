package store

import (
	"context"
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// LdapSyncLog is a row of the ldap_sync_log audit table.
type LdapSyncLog struct {
	ID             int64   `json:"id" db:"id"`
	SyncType       string  `json:"sync_type" db:"sync_type"`
	StartedAt      string  `json:"started_at" db:"started_at"`
	FinishedAt     *string `json:"finished_at,omitempty" db:"finished_at"`
	Status         string  `json:"status" db:"status"`
	ItemsProcessed int     `json:"items_processed" db:"items_processed"`
	ItemsCreated   int     `json:"items_created" db:"items_created"`
	ItemsUpdated   int     `json:"items_updated" db:"items_updated"`
	ItemsFailed    int     `json:"items_failed" db:"items_failed"`
	ErrorMessage   *string `json:"error_message,omitempty" db:"error_message"`
}

// LdapSyncItem is a per-entity row of an ldap sync (ldap_attributes as raw JSON).
type LdapSyncItem struct {
	ID             int64           `db:"id"`
	EntityType     string          `db:"entity_type"`
	EntityName     string          `db:"entity_name"`
	EntityID       *int64          `db:"entity_id"`
	LdapDN         string          `db:"ldap_dn"`
	LdapAttributes json.RawMessage `db:"ldap_attributes"`
	Action         string          `db:"action"`
	ErrorMessage   *string         `db:"error_message"`
	CreatedAt      string          `db:"created_at"`
}

// LdapStore is the data-access layer for LDAP sync-log reads.
type LdapStore struct {
	db *sqlx.DB
}

func NewLdapStore(db *sqlx.DB) *LdapStore { return &LdapStore{db: db} }

// RecentSyncLogs returns the most recent sync-log entries.
func (s *LdapStore) RecentSyncLogs(ctx context.Context, limit int) ([]LdapSyncLog, error) {
	logs := []LdapSyncLog{}
	err := s.db.SelectContext(ctx, &logs, `
		SELECT id, sync_type, started_at, finished_at, status,
		       items_processed, items_created, items_updated, items_failed, error_message
		FROM ldap_sync_log
		ORDER BY started_at DESC
		LIMIT $1`, limit)
	return logs, wrap("LdapStore.RecentSyncLogs", err)
}

// SyncLog returns a single sync-log entry by id.
func (s *LdapStore) SyncLog(ctx context.Context, id int64) (LdapSyncLog, error) {
	var log LdapSyncLog
	err := s.db.GetContext(ctx, &log, `
		SELECT id, sync_type, started_at, finished_at, status,
		       items_processed, items_created, items_updated, items_failed, error_message
		FROM ldap_sync_log WHERE id = $1`, id)
	return log, wrap("LdapStore.SyncLog", err)
}

// SyncItems returns the per-entity items of a sync, ordered by type then name.
func (s *LdapStore) SyncItems(ctx context.Context, syncLogID int64) ([]LdapSyncItem, error) {
	items := []LdapSyncItem{}
	err := s.db.SelectContext(ctx, &items, `
		SELECT id, entity_type, entity_name, entity_id, ldap_dn, ldap_attributes, action, error_message, created_at
		FROM ldap_sync_items
		WHERE sync_log_id = $1
		ORDER BY entity_type, entity_name`, syncLogID)
	return items, wrap("LdapStore.SyncItems", err)
}
