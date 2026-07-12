package handlers

import (
	"net/http"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/auth"
	"github.com/praetordev/praetor/pkg/render"
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

	// Render the maps as the operator wrote them (the AUTH_LDAP_*_MAP config), so
	// the UI can show the raw LDAP query. matchJSON reproduces the original
	// bool / single-DN / list-of-DNs shape; each configured role also carries its
	// remove_* flag. Unconfigured roles are omitted (no group bound to them). No
	// secrets are involved — these are group DNs.
	matchJSON := func(m auth.GroupMatch) interface{} {
		if m.All != nil {
			return *m.All
		}
		if len(m.DNs) == 1 {
			return m.DNs[0]
		}
		return m.DNs
	}
	orgMap := make(map[string]interface{}, len(cfg.OrganizationMap))
	for name, e := range cfg.OrganizationMap {
		entry := map[string]interface{}{}
		if e.Admins.Configured() {
			entry["admins"] = matchJSON(e.Admins)
			entry["remove_admins"] = e.RemoveAdmins
		}
		if e.Users.Configured() {
			entry["users"] = matchJSON(e.Users)
			entry["remove_users"] = e.RemoveUsers
		}
		if e.Auditors.Configured() {
			entry["auditors"] = matchJSON(e.Auditors)
			entry["remove_auditors"] = e.RemoveAuditors
		}
		orgMap[name] = entry
	}
	teamMap := make(map[string]interface{}, len(cfg.TeamMap))
	for name, e := range cfg.TeamMap {
		entry := map[string]interface{}{
			"organization": e.Organization,
		}
		if e.Users.Configured() {
			entry["users"] = matchJSON(e.Users)
			entry["remove"] = e.Remove
		}
		teamMap[name] = entry
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
