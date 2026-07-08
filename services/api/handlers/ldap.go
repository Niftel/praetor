package handlers

import (
	"net/http"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/auth"
	"github.com/praetordev/praetor/services/api/render"
)

// LDAPHandler serves the read-only LDAP config + connection-test endpoints. There
// is no sync: users/orgs/teams are mapped at login (see pkg/auth/LDAP-REDESIGN.md).
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
// Returns the current LDAP configuration (without secrets), including the AAP
// group→role mapping summary.
func (h *LDAPHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat(h.ConfigPath); os.IsNotExist(err) {
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

	// Summarize the maps without leaking secrets. DN lists are safe to show.
	orgMap := make(map[string]interface{}, len(cfg.OrganizationMap))
	for name, e := range cfg.OrganizationMap {
		orgMap[name] = map[string]interface{}{
			"admins":   e.Admins.DNs,
			"users":    e.Users.DNs,
			"auditors": e.Auditors.DNs,
		}
	}
	teamMap := make(map[string]interface{}, len(cfg.TeamMap))
	for name, e := range cfg.TeamMap {
		teamMap[name] = map[string]interface{}{
			"organization": e.Organization,
			"users":        e.Users.DNs,
		}
	}

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
		"group_type": map[string]interface{}{
			"type":        cfg.GroupType.Type,
			"search_base": cfg.GroupType.SearchBase,
		},
		"user_flags_by_group": map[string]interface{}{
			"is_superuser":      cfg.UserFlags.IsSuperuser.DNs,
			"is_system_auditor": cfg.UserFlags.IsSystemAuditor.DNs,
		},
		"organization_map": orgMap,
		"team_map":         teamMap,
	})
}
