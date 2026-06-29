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

	// Org search defaults
	if c.Organizations.Enabled && c.Organizations.SearchScope == "" {
		c.Organizations.SearchScope = SearchScopeOne
	}
	if c.Organizations.Enabled && c.Organizations.SearchFilter == "" {
		c.Organizations.SearchFilter = "(objectClass=organizationalUnit)"
	}
	if c.Organizations.Enabled && c.Organizations.Attributes.Name == "" {
		c.Organizations.Attributes.Name = "ou"
	}
	if c.Organizations.Enabled && c.Organizations.MemberAttribute == "" {
		c.Organizations.MemberAttribute = "member"
	}

	// Team search defaults
	if c.Teams.Enabled && c.Teams.SearchScope == "" {
		c.Teams.SearchScope = SearchScopeSub
	}
	if c.Teams.Enabled && c.Teams.SearchFilter == "" {
		c.Teams.SearchFilter = "(objectClass=groupOfNames)"
	}
	if c.Teams.Enabled && c.Teams.Attributes.Name == "" {
		c.Teams.Attributes.Name = "cn"
	}
	if c.Teams.Enabled && c.Teams.MemberAttribute == "" {
		c.Teams.MemberAttribute = "member"
	}

	// Sync defaults
	if c.Sync.Interval == 0 {
		c.Sync.Interval = time.Hour
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

	// Organization validation
	if c.Organizations.Enabled {
		if c.Organizations.SearchBase == "" {
			errs = append(errs, "organizations.search_base is required when organizations.enabled is true")
		}
		if err := validateSearchScope(c.Organizations.SearchScope); err != nil {
			errs = append(errs, fmt.Sprintf("organizations.search_scope: %v", err))
		}
	}

	// Team validation
	if c.Teams.Enabled {
		if c.Teams.SearchBase == "" {
			errs = append(errs, "teams.search_base is required when teams.enabled is true")
		}
		if err := validateSearchScope(c.Teams.SearchScope); err != nil {
			errs = append(errs, fmt.Sprintf("teams.search_scope: %v", err))
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
