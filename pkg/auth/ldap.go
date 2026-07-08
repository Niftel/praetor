package auth

import (
	"time"
)

// LDAPConfig is the root configuration structure for LDAP integration.
//
// The Organizations/Teams/Sync fields drive the legacy OU-discovery sync and are
// being removed (see pkg/auth/LDAP-REDESIGN.md). The GroupType/UserFlags/
// OrganizationMap/TeamMap fields drive the AAP/AWX login-time group→role model
// that replaces it: LDAP groups are bound to Praetor roles, evaluated at login.
type LDAPConfig struct {
	Server        LDAPServerConfig `yaml:"server"`
	Users         LDAPUserConfig   `yaml:"users"`
	Organizations LDAPOrgConfig    `yaml:"organizations"` // DEPRECATED (legacy sync)
	Teams         LDAPTeamConfig   `yaml:"teams"`         // DEPRECATED (legacy sync)
	Sync          LDAPSyncConfig   `yaml:"sync"`          // DEPRECATED (legacy sync)

	// AAP/AWX login-time mapping.
	GroupType       LDAPGroupTypeConfig         `yaml:"group_type"`
	UserFlags       LDAPUserFlagsConfig         `yaml:"user_flags_by_group"`
	OrganizationMap map[string]LDAPOrgMapEntry  `yaml:"organization_map"` // key = Praetor org NAME
	TeamMap         map[string]LDAPTeamMapEntry `yaml:"team_map"`         // key = Praetor team NAME
}

// UsesLoginMapping reports whether any AAP-style mapping is configured, i.e. the
// new model is in use for this config.
func (c *LDAPConfig) UsesLoginMapping() bool {
	return len(c.OrganizationMap) > 0 || len(c.TeamMap) > 0 ||
		c.UserFlags.IsSuperuser.Configured() || c.UserFlags.IsSystemAuditor.Configured()
}

// LDAPServerConfig contains LDAP server connection settings.
type LDAPServerConfig struct {
	URL                string        `yaml:"url"`                  // ldap:// or ldaps:// URL
	BindDN             string        `yaml:"bind_dn"`              // Service account DN
	BindPassword       string        `yaml:"bind_password"`        // Direct password (not recommended)
	BindPasswordEnv    string        `yaml:"bind_password_env"`    // Environment variable name for password
	StartTLS           bool          `yaml:"start_tls"`            // Use StartTLS
	InsecureSkipVerify bool          `yaml:"insecure_skip_verify"` // Skip TLS verification (not recommended)
	Timeout            time.Duration `yaml:"timeout"`              // Connection timeout
	PageSize           int           `yaml:"page_size"`            // Results per page (0 = no paging)
	FollowReferrals    bool          `yaml:"follow_referrals"`     // Chase LDAP referrals
}

// LDAPSearchScope defines the scope of an LDAP search.
type LDAPSearchScope string

const (
	SearchScopeBase LDAPSearchScope = "base" // Search only the base DN
	SearchScopeOne  LDAPSearchScope = "one"  // Search one level below base DN
	SearchScopeSub  LDAPSearchScope = "sub"  // Search entire subtree
)

// LDAPUserConfig configures how users are sourced from LDAP.
type LDAPUserConfig struct {
	SearchBase   string             `yaml:"search_base"`   // Base DN for user search (single, backwards compat)
	SearchBases  []string           `yaml:"search_bases"`  // Multiple base DNs for user search
	SearchFilter string             `yaml:"search_filter"` // LDAP filter for users
	SearchScope  LDAPSearchScope    `yaml:"search_scope"`  // Search scope
	Attributes   LDAPUserAttributes `yaml:"attributes"`    // Attribute mappings
}

// LDAPUserAttributes maps LDAP attributes to Praetor user fields.
type LDAPUserAttributes struct {
	Username  string            `yaml:"username"`   // Attribute for username (e.g., "uid", "sAMAccountName")
	Email     string            `yaml:"email"`      // Attribute for email (e.g., "mail")
	FirstName string            `yaml:"first_name"` // Attribute for first name (e.g., "givenName")
	LastName  string            `yaml:"last_name"`  // Attribute for last name (e.g., "sn")
	Custom    map[string]string `yaml:"custom"`     // Custom attribute mappings (praetor_field -> ldap_attr)
}

// LDAPOrgConfig configures how organizations are sourced from LDAP.
type LDAPOrgConfig struct {
	Enabled         bool              `yaml:"enabled"`          // Whether to sync orgs from LDAP
	SearchBase      string            `yaml:"search_base"`      // Base DN for org search (single, backwards compat)
	SearchBases     []string          `yaml:"search_bases"`     // Multiple base DNs for org search
	SearchFilter    string            `yaml:"search_filter"`    // LDAP filter for orgs
	SearchScope     LDAPSearchScope   `yaml:"search_scope"`     // Search scope
	Attributes      LDAPOrgAttributes `yaml:"attributes"`       // Attribute mappings
	MemberAttribute string            `yaml:"member_attribute"` // Attribute containing member DNs
}

// LDAPOrgAttributes maps LDAP attributes to Praetor organization fields.
type LDAPOrgAttributes struct {
	Name        string `yaml:"name"`        // Attribute for org name (e.g., "ou", "cn")
	Description string `yaml:"description"` // Attribute for description
}

// LDAPTeamConfig configures how teams are sourced from LDAP.
type LDAPTeamConfig struct {
	Enabled               bool                  `yaml:"enabled"`                // Whether to sync teams from LDAP
	SearchBase            string                `yaml:"search_base"`            // Base DN for team search (single, backwards compat)
	SearchBases           []string              `yaml:"search_bases"`           // Multiple base DNs for team search
	SearchFilter          string                `yaml:"search_filter"`          // LDAP filter for teams
	SearchScope           LDAPSearchScope       `yaml:"search_scope"`           // Search scope
	Attributes            LDAPTeamAttributes    `yaml:"attributes"`             // Attribute mappings
	MemberAttribute       string                `yaml:"member_attribute"`       // Attribute containing member DNs
	OrganizationAttribute string                `yaml:"organization_attribute"` // Attribute linking team to org
	NestedGroups          LDAPNestedGroupConfig `yaml:"nested_groups"`          // Nested group resolution config
}

// LDAPNestedGroupConfig configures nested group resolution.
type LDAPNestedGroupConfig struct {
	Enabled           bool   `yaml:"enabled"`             // Whether to resolve nested groups
	MaxDepth          int    `yaml:"max_depth"`           // Max recursion depth (default: 5)
	MemberOfAttribute string `yaml:"member_of_attribute"` // Attribute for group membership (e.g., "memberOf")
}

// LDAPTeamAttributes maps LDAP attributes to Praetor team fields.
type LDAPTeamAttributes struct {
	Name        string `yaml:"name"`        // Attribute for team name (e.g., "cn")
	Description string `yaml:"description"` // Attribute for description
}

// LDAPSyncConfig configures sync behavior.
type LDAPSyncConfig struct {
	Interval    time.Duration `yaml:"interval"`     // Sync interval (0 = disabled)
	CreateUsers bool          `yaml:"create_users"` // Create users if they don't exist
	CreateOrgs  bool          `yaml:"create_orgs"`  // Create orgs if they don't exist
	CreateTeams bool          `yaml:"create_teams"` // Create teams if they don't exist
	RemoveStale bool          `yaml:"remove_stale"` // Remove entities not in LDAP
	DryRun      bool          `yaml:"dry_run"`      // Log changes without applying
}

// LDAPSyncResult contains the results of a sync operation.
type LDAPSyncResult struct {
	ID             int64          `json:"id,omitempty" db:"id"`
	SyncType       string         `json:"sync_type" db:"sync_type"` // "users", "organizations", "teams", "full"
	StartedAt      time.Time      `json:"started_at" db:"started_at"`
	FinishedAt     time.Time      `json:"finished_at" db:"finished_at"`
	Status         string         `json:"status" db:"status"` // "success", "failed", "partial"
	ItemsProcessed int            `json:"items_processed" db:"items_processed"`
	ItemsCreated   int            `json:"items_created" db:"items_created"`
	ItemsUpdated   int            `json:"items_updated" db:"items_updated"`
	ItemsFailed    int            `json:"items_failed" db:"items_failed"`
	ErrorMessage   *string        `json:"error_message,omitempty" db:"error_message"`
	Errors         []string       `json:"errors,omitempty" db:"-"`
	Items          []LDAPSyncItem `json:"items,omitempty" db:"-"`
}

// LDAPSyncItem represents a single item that was synced.
type LDAPSyncItem struct {
	ID             int64               `json:"id" db:"id"`
	SyncLogID      int64               `json:"sync_log_id" db:"sync_log_id"`
	EntityType     string              `json:"entity_type" db:"entity_type"` // "user", "organization", "team"
	EntityName     string              `json:"entity_name" db:"entity_name"` // username, org name, or team name
	EntityID       *int64              `json:"entity_id,omitempty" db:"entity_id"`
	LdapDN         string              `json:"ldap_dn" db:"ldap_dn"`                           // LDAP Distinguished Name
	LdapAttributes map[string][]string `json:"ldap_attributes,omitempty" db:"ldap_attributes"` // Raw LDAP attributes
	Action         string              `json:"action" db:"action"`                             // "created", "updated", "unchanged", "failed"
	ErrorMessage   *string             `json:"error_message,omitempty" db:"error_message"`
	CreatedAt      time.Time           `json:"created_at" db:"created_at"`
}

// LDAPEntry represents a single LDAP entry with its DN and attributes.
type LDAPEntry struct {
	DN         string
	Attributes map[string][]string
}

// GetAttribute returns the first value for the given attribute, or empty string.
func (e *LDAPEntry) GetAttribute(name string) string {
	if vals, ok := e.Attributes[name]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// GetAttributes returns all values for the given attribute.
func (e *LDAPEntry) GetAttributes(name string) []string {
	if vals, ok := e.Attributes[name]; ok {
		return vals
	}
	return nil
}

// GetSearchBases returns all search bases for users (backwards compatible).
func (c *LDAPUserConfig) GetSearchBases() []string {
	if len(c.SearchBases) > 0 {
		return c.SearchBases
	}
	if c.SearchBase != "" {
		return []string{c.SearchBase}
	}
	return nil
}

// GetSearchBases returns all search bases for organizations (backwards compatible).
func (c *LDAPOrgConfig) GetSearchBases() []string {
	if len(c.SearchBases) > 0 {
		return c.SearchBases
	}
	if c.SearchBase != "" {
		return []string{c.SearchBase}
	}
	return nil
}

// GetSearchBases returns all search bases for teams (backwards compatible).
func (c *LDAPTeamConfig) GetSearchBases() []string {
	if len(c.SearchBases) > 0 {
		return c.SearchBases
	}
	if c.SearchBase != "" {
		return []string{c.SearchBase}
	}
	return nil
}
