package auth

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads LDAP configuration from a YAML file.
func LoadConfig(path string) (*LDAPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	return ParseConfig(data)
}

// ParseConfig parses LDAP configuration from YAML bytes.
func ParseConfig(data []byte) (*LDAPConfig, error) {
	var cfg LDAPConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Substitute environment variables for secrets
	if err := cfg.substituteEnvVars(); err != nil {
		return nil, err
	}

	// Apply defaults
	cfg.applyDefaults()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// substituteEnvVars replaces environment variable references with actual values.
func (c *LDAPConfig) substituteEnvVars() error {
	if c.Server.BindPasswordEnv != "" {
		password := os.Getenv(c.Server.BindPasswordEnv)
		if password == "" {
			return fmt.Errorf("environment variable %s not set", c.Server.BindPasswordEnv)
		}
		c.Server.BindPassword = password
	}
	return nil
}

// applyDefaults sets default values for unspecified fields.
func (c *LDAPConfig) applyDefaults() {
	// Server defaults
	if c.Server.Timeout == 0 {
		c.Server.Timeout = 30 * time.Second
	}

	// User search defaults
	if c.Users.SearchScope == "" {
		c.Users.SearchScope = SearchScopeSub
	}
	if c.Users.SearchFilter == "" {
		c.Users.SearchFilter = "(objectClass=person)"
	}
	if c.Users.Attributes.Username == "" {
		c.Users.Attributes.Username = "uid"
	}
	if c.Users.Attributes.Email == "" {
		c.Users.Attributes.Email = "mail"
	}
	if c.Users.Attributes.FirstName == "" {
		c.Users.Attributes.FirstName = "givenName"
	}
	if c.Users.Attributes.LastName == "" {
		c.Users.Attributes.LastName = "sn"
	}

	// Group-type (AAP login-mapping) defaults, applied only when a type is set.
	if c.GroupType.Type != "" {
		if c.GroupType.SearchFilter == "" {
			c.GroupType.SearchFilter = "(objectClass=groupOfNames)"
		}
		if c.GroupType.MemberAttribute == "" {
			if c.GroupType.Type == GroupTypePosix {
				c.GroupType.MemberAttribute = "memberUid"
			} else {
				c.GroupType.MemberAttribute = "member"
			}
		}
		if c.GroupType.MemberOfAttribute == "" {
			c.GroupType.MemberOfAttribute = "memberOf"
		}
		if c.GroupType.MaxDepth == 0 {
			c.GroupType.MaxDepth = 5
		}
	}
}

// Validate checks the configuration for required fields and valid values.
func (c *LDAPConfig) Validate() error {
	var errs []string

	// Server validation
	if c.Server.URL == "" {
		errs = append(errs, "server.url is required")
	} else if !strings.HasPrefix(c.Server.URL, "ldap://") && !strings.HasPrefix(c.Server.URL, "ldaps://") {
		errs = append(errs, "server.url must start with ldap:// or ldaps://")
	}

	if c.Server.BindDN == "" {
		errs = append(errs, "server.bind_dn is required")
	}

	if c.Server.BindPassword == "" {
		errs = append(errs, "server.bind_password or server.bind_password_env is required")
	}

	// User validation
	if c.Users.SearchBase == "" {
		errs = append(errs, "users.search_base is required")
	}

	if err := validateSearchScope(c.Users.SearchScope); err != nil {
		errs = append(errs, fmt.Sprintf("users.search_scope: %v", err))
	}

	// AAP login-mapping validation (only when the new model is configured).
	if c.UsesLoginMapping() {
		switch c.GroupType.Type {
		case GroupTypeMemberDN, GroupTypeMemberOf, GroupTypePosix, GroupTypeNested:
			// ok
		case "":
			errs = append(errs, "group_type.type is required when organization_map/team_map/user_flags_by_group are set")
		default:
			errs = append(errs, fmt.Sprintf("group_type.type %q must be one of: member_dn, member_of, posix, nested", c.GroupType.Type))
		}
		if c.GroupType.Type != "" && c.GroupType.Type != GroupTypeMemberOf && c.GroupType.SearchBase == "" {
			errs = append(errs, fmt.Sprintf("group_type.search_base is required for group_type %q", c.GroupType.Type))
		}
		for name, entry := range c.TeamMap {
			if entry.Organization == "" {
				errs = append(errs, fmt.Sprintf("team_map[%q].organization is required", name))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

func validateSearchScope(scope LDAPSearchScope) error {
	switch scope {
	case SearchScopeBase, SearchScopeOne, SearchScopeSub:
		return nil
	default:
		return fmt.Errorf("invalid search scope %q, must be one of: base, one, sub", scope)
	}
}
