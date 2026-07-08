package auth

import (
	"testing"
	"time"
)

func TestParseConfig_ValidConfig(t *testing.T) {
	yaml := `
server:
  url: "ldaps://ldap.example.com:636"
  bind_dn: "cn=admin,dc=example,dc=com"
  bind_password: "secret"
  timeout: 30s

users:
  search_base: "ou=users,dc=example,dc=com"
  search_filter: "(objectClass=person)"
  search_scope: "sub"
  attributes:
    username: "uid"
    email: "mail"
    first_name: "givenName"
    last_name: "sn"
`

	cfg, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	// Verify server settings
	if cfg.Server.URL != "ldaps://ldap.example.com:636" {
		t.Errorf("expected URL ldaps://ldap.example.com:636, got %s", cfg.Server.URL)
	}
	if cfg.Server.BindDN != "cn=admin,dc=example,dc=com" {
		t.Errorf("expected BindDN cn=admin,dc=example,dc=com, got %s", cfg.Server.BindDN)
	}
	if cfg.Server.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cfg.Server.Timeout)
	}

	// Verify user settings
	if cfg.Users.SearchBase != "ou=users,dc=example,dc=com" {
		t.Errorf("expected users search_base ou=users,dc=example,dc=com, got %s", cfg.Users.SearchBase)
	}
	if cfg.Users.SearchScope != SearchScopeSub {
		t.Errorf("expected search_scope sub, got %s", cfg.Users.SearchScope)
	}
}

func TestParseConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantError string
	}{
		{
			name: "missing server URL",
			yaml: `
server:
  bind_dn: "cn=admin"
  bind_password: "secret"
users:
  search_base: "ou=users"
`,
			wantError: "server.url is required",
		},
		{
			name: "invalid URL scheme",
			yaml: `
server:
  url: "http://ldap.example.com"
  bind_dn: "cn=admin"
  bind_password: "secret"
users:
  search_base: "ou=users"
`,
			wantError: "server.url must start with ldap:// or ldaps://",
		},
		{
			name: "missing bind_dn",
			yaml: `
server:
  url: "ldap://ldap.example.com"
  bind_password: "secret"
users:
  search_base: "ou=users"
`,
			wantError: "server.bind_dn is required",
		},
		{
			name: "invalid search scope",
			yaml: `
server:
  url: "ldap://ldap.example.com"
  bind_dn: "cn=admin"
  bind_password: "secret"
users:
  search_base: "ou=users"
  search_scope: "invalid"
`,
			wantError: "invalid search scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tt.yaml))
			if err == nil {
				t.Error("expected validation error, got nil")
				return
			}
			if tt.wantError != "" && !containsString(err.Error(), tt.wantError) {
				t.Errorf("expected error containing %q, got %q", tt.wantError, err.Error())
			}
		})
	}
}

func TestParseConfig_Defaults(t *testing.T) {
	yaml := `
server:
  url: "ldap://ldap.example.com"
  bind_dn: "cn=admin"
  bind_password: "secret"
users:
  search_base: "ou=users"
`

	cfg, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	// Check defaults are applied
	if cfg.Server.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", cfg.Server.Timeout)
	}
	if cfg.Users.SearchScope != SearchScopeSub {
		t.Errorf("expected default search_scope sub, got %s", cfg.Users.SearchScope)
	}
	if cfg.Users.Attributes.Username != "uid" {
		t.Errorf("expected default username attr uid, got %s", cfg.Users.Attributes.Username)
	}
}

func TestLDAPEntry_GetAttribute(t *testing.T) {
	entry := &LDAPEntry{
		DN: "cn=user,ou=users,dc=example,dc=com",
		Attributes: map[string][]string{
			"cn":   {"user"},
			"mail": {"user@example.com"},
			"memberOf": {
				"cn=group1,ou=groups,dc=example,dc=com",
				"cn=group2,ou=groups,dc=example,dc=com",
			},
		},
	}

	if got := entry.GetAttribute("cn"); got != "user" {
		t.Errorf("GetAttribute(cn) = %q, want %q", got, "user")
	}

	if got := entry.GetAttribute("nonexistent"); got != "" {
		t.Errorf("GetAttribute(nonexistent) = %q, want empty string", got)
	}

	memberOf := entry.GetAttributes("memberOf")
	if len(memberOf) != 2 {
		t.Errorf("GetAttributes(memberOf) got %d values, want 2", len(memberOf))
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
