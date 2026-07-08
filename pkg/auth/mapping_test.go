package auth

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func mustSet(t *testing.T, dns ...string) map[string]struct{} {
	t.Helper()
	return normalizeDNSet(dns)
}

func TestGroupMatchUnmarshal(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantAll *bool
		wantDNs int
	}{
		{"bool true", "true", boolp(true), 0},
		{"bool false", "false", boolp(false), 0},
		{"single dn", `"cn=admins,dc=x"`, nil, 1},
		{"list", "[\"cn=a,dc=x\", \"cn=b,dc=x\"]", nil, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m GroupMatch
			if err := yaml.Unmarshal([]byte(tc.yaml), &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if (tc.wantAll == nil) != (m.All == nil) {
				t.Fatalf("All mismatch: got %v want %v", m.All, tc.wantAll)
			}
			if tc.wantAll != nil && *m.All != *tc.wantAll {
				t.Fatalf("All value: got %v want %v", *m.All, *tc.wantAll)
			}
			if len(m.DNs) != tc.wantDNs {
				t.Fatalf("DNs: got %d want %d", len(m.DNs), tc.wantDNs)
			}
		})
	}
}

func TestGroupMatchMatches(t *testing.T) {
	groups := mustSet(t, "CN=Admins,OU=Teams,DC=x") // mixed case in the directory

	allTrue := GroupMatch{All: boolp(true)}
	allFalse := GroupMatch{All: boolp(false)}
	byDN := GroupMatch{DNs: []string{"cn=admins,ou=teams,dc=x"}} // lower case in config
	miss := GroupMatch{DNs: []string{"cn=nobody,dc=x"}}

	if !allTrue.Matches(groups) {
		t.Error("All=true should match")
	}
	if allFalse.Matches(groups) {
		t.Error("All=false should not match")
	}
	if !byDN.Matches(groups) {
		t.Error("DN match should be case-insensitive")
	}
	if miss.Matches(groups) {
		t.Error("non-member DN should not match")
	}
	if !byDN.Configured() || allFalse.Configured() != true || (GroupMatch{}).Configured() {
		t.Error("Configured semantics wrong")
	}
}

func TestGroupDNListResolve(t *testing.T) {
	groups := mustSet(t, "cn=supers,dc=x")

	// Unconfigured: never assign (unset ≠ false).
	if _, assign := (GroupDNList{}).Resolve(groups); assign {
		t.Error("unconfigured flag must not be assigned")
	}
	// Configured + match → true, assign.
	if v, assign := (GroupDNList{DNs: []string{"cn=supers,dc=x"}}).Resolve(groups); !assign || !v {
		t.Errorf("match: got v=%v assign=%v", v, assign)
	}
	// Configured + no match → false, assign.
	if v, assign := (GroupDNList{DNs: []string{"cn=other,dc=x"}}).Resolve(groups); !assign || v {
		t.Errorf("no-match: got v=%v assign=%v", v, assign)
	}
}

func TestDecideRole(t *testing.T) {
	cases := []struct {
		matched, configured, remove bool
		grant, revoke               bool
	}{
		{true, true, true, true, false},    // match → grant
		{true, true, false, true, false},   // match → grant (remove irrelevant)
		{false, true, true, false, true},   // no match + configured + remove → revoke
		{false, true, false, false, false}, // no match, remove off → grant-only, no-op
		{false, false, true, false, false}, // unconfigured → never revoke
	}
	for _, c := range cases {
		g, r := decideRole(c.matched, c.configured, c.remove)
		if g != c.grant || r != c.revoke {
			t.Errorf("decideRole(%v,%v,%v) = (%v,%v) want (%v,%v)",
				c.matched, c.configured, c.remove, g, r, c.grant, c.revoke)
		}
	}
}

func TestNormalizeDN(t *testing.T) {
	a := NormalizeDN("CN=Admins, OU=Teams, DC=Praetor, DC=Local")
	b := NormalizeDN("cn=admins,ou=teams,dc=praetor,dc=local")
	if a != b {
		t.Errorf("normalization mismatch:\n %q\n %q", a, b)
	}
}

const validServerUsers = `
server:
  url: "ldap://ldap:389"
  bind_dn: "cn=admin,dc=x"
  bind_password: "secret"
users:
  search_base: "ou=users,dc=x"
`

func TestParseConfigNewShapeValid(t *testing.T) {
	doc := validServerUsers + `
group_type:
  type: member_dn
  search_base: "ou=teams,dc=x"
user_flags_by_group:
  is_superuser: "cn=admins,ou=teams,dc=x"
organization_map:
  Engineering:
    admins: ["cn=eng-admins,ou=teams,dc=x"]
    users: true
    remove_users: true
team_map:
  platform:
    organization: Engineering
    users: ["cn=platform,ou=teams,dc=x"]
`
	cfg, err := ParseConfig([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.UsesLoginMapping() {
		t.Error("expected UsesLoginMapping true")
	}
	if cfg.GroupType.MemberAttribute != "member" || cfg.GroupType.MaxDepth != 5 {
		t.Errorf("defaults not applied: %+v", cfg.GroupType)
	}
	if len(cfg.UserFlags.IsSuperuser.DNs) != 1 {
		t.Errorf("is_superuser dns: %+v", cfg.UserFlags.IsSuperuser)
	}
	if u := cfg.OrganizationMap["Engineering"].Users; u.All == nil || !*u.All {
		t.Errorf("org Engineering users should be All=true: %+v", u)
	}
}

func TestParseConfigNewShapeInvalid(t *testing.T) {
	// group_type.type missing search_base, and team_map missing organization.
	doc := validServerUsers + `
group_type:
  type: member_dn
organization_map:
  Ops: { users: true }
team_map:
  sre:
    users: ["cn=sre,dc=x"]
`
	_, err := ParseConfig([]byte(doc))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func boolp(b bool) *bool { return &b }
