package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/auth"
	"github.com/praetordev/praetor/services/api/render"
)

// LDAPHandler handles LDAP sync and configuration endpoints.
type LDAPHandler struct {
	DB         *sqlx.DB
	ConfigPath string
}

// NewLDAPHandler creates a new LDAP handler. configPath is resolved in main from
// env (empty falls back to the in-cluster default).
func NewLDAPHandler(db *sqlx.DB, configPath string) *LDAPHandler {
	if configPath == "" {
		configPath = "/etc/praetor/ldap.yaml"
	}
	return &LDAPHandler{
		DB:         db,
		ConfigPath: configPath,
	}
}

// TriggerSync POST /api/v1/ldap/sync
// Triggers a full LDAP sync operation.
func (h *LDAPHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	cfg, err := auth.LoadConfig(h.ConfigPath)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	client := auth.NewLDAPClient(cfg)
	syncer := auth.NewSyncer(client, h.DB, cfg)

	result, err := syncer.SyncAll(context.Background())
	if err != nil {
		// Still return the result with error details
		render.JSON(w, r, result)
		return
	}

	render.JSON(w, r, result)
}

// SyncRequest allows specifying which entities to sync.
type SyncRequest struct {
	Type string `json:"type"` // "users", "organizations", "teams", or "full" (default)
}

// TriggerSyncSpecific POST /api/v1/ldap/sync/{type}
// Triggers a specific LDAP sync operation.
func (h *LDAPHandler) TriggerSyncSpecific(w http.ResponseWriter, r *http.Request) {
	var req SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Type = "full" // Default to full sync
	}

	cfg, err := auth.LoadConfig(h.ConfigPath)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	client := auth.NewLDAPClient(cfg)
	syncer := auth.NewSyncer(client, h.DB, cfg)

	var result *auth.LDAPSyncResult
	ctx := context.Background()

	switch req.Type {
	case "users":
		result, err = syncer.SyncUsers(ctx)
	case "organizations":
		result, err = syncer.SyncOrganizations(ctx)
	case "teams":
		result, err = syncer.SyncTeams(ctx)
	default:
		result, err = syncer.SyncAll(ctx)
	}

	if err != nil {
		render.JSON(w, r, result)
		return
	}

	render.JSON(w, r, result)
}

// GetSyncStatus GET /api/v1/ldap/sync/status
// Returns the status of recent sync operations.
func (h *LDAPHandler) GetSyncStatus(w http.ResponseWriter, r *http.Request) {
	type SyncLogEntry struct {
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

	var logs []SyncLogEntry
	err := h.DB.Select(&logs, `
		SELECT id, sync_type, started_at, finished_at, status, 
		       items_processed, items_created, items_updated, items_failed, error_message
		FROM ldap_sync_log
		ORDER BY started_at DESC
		LIMIT 20
	`)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if logs == nil {
		logs = []SyncLogEntry{}
	}

	render.JSON(w, r, map[string]interface{}{
		"results": logs,
		"count":   len(logs),
	})
}

// GetSyncDetails GET /api/v1/ldap/sync/{id}
// Returns detailed items for a specific sync operation.
func (h *LDAPHandler) GetSyncDetails(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Get the sync log entry
	type SyncLogEntry struct {
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

	var log SyncLogEntry
	err = h.DB.Get(&log, `
		SELECT id, sync_type, started_at, finished_at, status,
		       items_processed, items_created, items_updated, items_failed, error_message
		FROM ldap_sync_log WHERE id = $1`, id)
	if err != nil {
		render.ErrNotFound(nil).Render(w, r)
		return
	}

	// Get the sync items - use RawMessage for JSONB field
	type SyncItemDB struct {
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

	var itemsDB []SyncItemDB
	err = h.DB.Select(&itemsDB, `
		SELECT id, entity_type, entity_name, entity_id, ldap_dn, ldap_attributes, action, error_message, created_at
		FROM ldap_sync_items
		WHERE sync_log_id = $1
		ORDER BY entity_type, entity_name`, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Convert to response format with parsed JSONB
	type SyncItem struct {
		ID             int64               `json:"id"`
		EntityType     string              `json:"entity_type"`
		EntityName     string              `json:"entity_name"`
		EntityID       *int64              `json:"entity_id,omitempty"`
		LdapDN         string              `json:"ldap_dn"`
		LdapAttributes map[string][]string `json:"ldap_attributes,omitempty"`
		Action         string              `json:"action"`
		ErrorMessage   *string             `json:"error_message,omitempty"`
		CreatedAt      string              `json:"created_at"`
	}

	items := make([]SyncItem, len(itemsDB))
	for i, dbItem := range itemsDB {
		items[i] = SyncItem{
			ID:           dbItem.ID,
			EntityType:   dbItem.EntityType,
			EntityName:   dbItem.EntityName,
			EntityID:     dbItem.EntityID,
			LdapDN:       dbItem.LdapDN,
			Action:       dbItem.Action,
			ErrorMessage: dbItem.ErrorMessage,
			CreatedAt:    dbItem.CreatedAt,
		}
		if len(dbItem.LdapAttributes) > 0 {
			json.Unmarshal(dbItem.LdapAttributes, &items[i].LdapAttributes)
		}
	}

	render.JSON(w, r, map[string]interface{}{
		"log":   log,
		"items": items,
	})
}

// TestConnection POST /api/v1/ldap/test-connection
// Tests the LDAP connection with current configuration.
func (h *LDAPHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	cfg, err := auth.LoadConfig(h.ConfigPath)
	if err != nil {
		render.JSON(w, r, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	client := auth.NewLDAPClient(cfg)
	if err := client.TestConnection(); err != nil {
		render.JSON(w, r, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"success": true,
		"message": "Successfully connected to LDAP server",
	})
}

// GetConfig GET /api/v1/ldap/config
// Returns the current LDAP configuration (without secrets).
func (h *LDAPHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(h.ConfigPath)
	if os.IsNotExist(err) {
		render.JSON(w, r, map[string]interface{}{
			"configured":  false,
			"config_path": h.ConfigPath,
		})
		return
	}

	cfg, err := auth.LoadConfig(h.ConfigPath)
	if err != nil {
		render.JSON(w, r, map[string]interface{}{
			"configured":   true,
			"config_path":  h.ConfigPath,
			"config_error": err.Error(),
		})
		return
	}

	// Return config without secrets
	render.JSON(w, r, map[string]interface{}{
		"configured":  true,
		"config_path": h.ConfigPath,
		"server": map[string]interface{}{
			"url":       cfg.Server.URL,
			"bind_dn":   cfg.Server.BindDN,
			"start_tls": cfg.Server.StartTLS,
			"timeout":   cfg.Server.Timeout.String(),
		},
		"users": map[string]interface{}{
			"search_base":   cfg.Users.SearchBase,
			"search_filter": cfg.Users.SearchFilter,
			"search_scope":  cfg.Users.SearchScope,
		},
		"organizations": map[string]interface{}{
			"enabled":       cfg.Organizations.Enabled,
			"search_base":   cfg.Organizations.SearchBase,
			"search_filter": cfg.Organizations.SearchFilter,
		},
		"teams": map[string]interface{}{
			"enabled":       cfg.Teams.Enabled,
			"search_base":   cfg.Teams.SearchBase,
			"search_filter": cfg.Teams.SearchFilter,
		},
		"sync": map[string]interface{}{
			"interval":     cfg.Sync.Interval.String(),
			"create_users": cfg.Sync.CreateUsers,
			"create_orgs":  cfg.Sync.CreateOrgs,
			"create_teams": cfg.Sync.CreateTeams,
			"remove_stale": cfg.Sync.RemoveStale,
			"dry_run":      cfg.Sync.DryRun,
		},
	})
}
