package auth

import (
	"time"
)

// LDAPConfig is the root configuration structure for LDAP integration.
//
// LDAP is evaluated at login (see pkg/auth/LDAP-REDESIGN.md): the GroupType/
// UserFlags/OrganizationMap/TeamMap fields bind LDAP groups to Praetor roles in
// the AAP/AWX style. The legacy OU-discovery sync has been removed.
type LDAPConfig struct {
	Server LDAPServerConfig `yaml:"server"`
	Users  LDAPUserConfig   `yaml:"users"`

	// AAP/AWX login-time mapping.
	GroupType       LDAPGroupTypeConfig         `yaml:"group_type"`
	UserFlags       LDAPUserFlagsConfig         `yaml:"user_flags_by_group"`
	OrganizationMap map[string]LDAPOrgMapEntry  `yaml:"organization_map"` // key = Praetor org NAME
	TeamMap         map[string]LDAPTeamMapEntry `yaml:"team_map"`         // key = Praetor team NAME

	// AuthenticatorMaps is the provider-neutral successor to the legacy LDAP-
	// specific maps above. It evaluates normalized groups and attributes and maps
	// them only to platform organizations, teams, and global roles.
	AuthenticatorMaps []AuthenticatorMap `yaml:"authenticator_maps"`
}

// UsesLoginMapping reports whether any AAP-style mapping is configured, i.e. the
// new model is in use for this config.
func (c *LDAPConfig) UsesLoginMapping() bool {
	return len(c.OrganizationMap) > 0 || len(c.TeamMap) > 0 ||
		len(c.AuthenticatorMaps) > 0 || c.UserFlags.IsSuperuser.Configured() || c.UserFlags.IsSystemAuditor.Configured()
}

// UsesGroupMapping reports whether configuration actually needs external group
// resolution. Attribute-only authenticator maps do not require group_type.
func (c *LDAPConfig) UsesGroupMapping() bool {
	if len(c.OrganizationMap) > 0 || len(c.TeamMap) > 0 ||
		c.UserFlags.IsSuperuser.Configured() || c.UserFlags.IsSystemAuditor.Configured() {
		return true
	}
	for _, m := range c.AuthenticatorMaps {
		if predicateUsesGroup(m.When) {
			return true
		}
	}
	return false
}

// LDAPServerConfig contains LDAP server connection settings.
type LDAPServerConfig struct {
	URL                string        `yaml:"url"`                  // ldap:// or ldaps:// URL
	CAFile             string        `yaml:"ca_file"`              // Optional PEM CA bundle for LDAP TLS verification
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
