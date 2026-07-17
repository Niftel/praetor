package auth

import (
	"strings"
	"testing"
)

func boolPointer(v bool) *bool { return &v }

func TestPredicateNestedBooleanLogic(t *testing.T) {
	production := "cn=production,ou=groups,dc=example,dc=com"
	operator := "cn=operators,ou=groups,dc=example,dc=com"
	suspended := "cn=suspended,ou=groups,dc=example,dc=com"
	p := Predicate{Any: []Predicate{
		{All: []Predicate{{Group: production}, {Group: operator}, {Not: &Predicate{Group: suspended}}}},
		{Attribute: &AttributePredicate{Name: "department", Equals: "platform"}},
	}}

	if err := validatePredicate(p, 1, new(int)); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !p.Matches(IdentityClaims{Groups: normalizeDNSet([]string{production, operator})}) {
		t.Fatal("expected production operator to match")
	}
	if p.Matches(IdentityClaims{Groups: normalizeDNSet([]string{production, operator, suspended})}) {
		t.Fatal("suspended operator must not match")
	}
	if !p.Matches(IdentityClaims{Groups: map[string]struct{}{}, Attributes: map[string][]string{"department": {"platform"}}}) {
		t.Fatal("expected exact attribute alternative to match")
	}
	if p.Matches(IdentityClaims{Groups: map[string]struct{}{}, Attributes: map[string][]string{}}) {
		t.Fatal("missing attributes must fail closed")
	}
}

func TestPredicateValidationRejectsAmbiguityAndLimits(t *testing.T) {
	cases := []Predicate{
		{},
		{Group: "cn=a", Always: boolPointer(true)},
		{All: []Predicate{}},
		{Attribute: &AttributePredicate{Name: "department"}},
	}
	for i, p := range cases {
		if err := validatePredicate(p, 1, new(int)); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}

	deep := Predicate{Group: "cn=a"}
	for i := 0; i < maxPredicateDepth; i++ {
		child := deep
		deep = Predicate{Not: &child}
	}
	if err := validatePredicate(deep, 1, new(int)); err == nil || !strings.Contains(err.Error(), "nesting depth") {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestAuthenticatorMapOrderingAndRevoke(t *testing.T) {
	group := "cn=operators,dc=example,dc=com"
	target := AuthenticatorGrant{Type: MapTeam, Organization: "Engineering", Team: "Operators", Role: "Team Member"}
	maps := []AuthenticatorMap{
		{Name: "grant operators", Order: 20, When: Predicate{Group: group}, Map: target, Revoke: true},
		{Name: "deny login by default", Order: 0, When: Predicate{Always: boolPointer(false)}, Map: AuthenticatorGrant{Type: MapAllow}, Revoke: true},
		{Name: "allow operators", Order: 10, When: Predicate{Group: group}, Map: AuthenticatorGrant{Type: MapAllow}},
	}
	if err := ValidateAuthenticatorMaps(maps); err != nil {
		t.Fatalf("validate: %v", err)
	}

	allow, decisions := evaluateAuthenticatorMaps(maps, IdentityClaims{Groups: normalizeDNSet([]string{group})})
	if !allow {
		t.Fatal("later allow rule should permit operator")
	}
	if d := decisions[grantKey(target)]; !d.on {
		t.Fatal("expected team grant")
	}

	allow, decisions = evaluateAuthenticatorMaps(maps, IdentityClaims{Groups: map[string]struct{}{}})
	if allow {
		t.Fatal("default-deny allow rule should reject non-member")
	}
	if d := decisions[grantKey(target)]; d.on {
		t.Fatal("authoritative non-match should revoke team grant")
	}
}

func TestParseConfigAuthenticatorMaps(t *testing.T) {
	doc := validServerUsers + `
group_type:
  type: member_dn
  search_base: "ou=groups,dc=x"
authenticator_maps:
  - name: production operators
    order: 10
    revoke: true
    when:
      all:
        - group: "cn=operators,ou=groups,dc=x"
        - not:
            group: "cn=suspended,ou=groups,dc=x"
    map:
      type: team
      organization: Engineering
      team: Operators
      role: Team Member
`
	cfg, err := ParseConfig([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.AuthenticatorMaps) != 1 || !cfg.UsesLoginMapping() {
		t.Fatalf("maps not loaded: %+v", cfg.AuthenticatorMaps)
	}
}

func TestParseConfigRejectsArbitraryResourceRoleMap(t *testing.T) {
	doc := validServerUsers + `
group_type:
  type: member_dn
  search_base: "ou=groups,dc=x"
authenticator_maps:
  - name: unsafe direct resource mapping
    order: 1
    when: { group: "cn=operators,dc=x" }
    map:
      type: role
      role: Workflow Template Admin
`
	if _, err := ParseConfig([]byte(doc)); err == nil || !strings.Contains(err.Error(), "only the global System Auditor role") {
		t.Fatalf("expected direct resource role rejection, got %v", err)
	}
}

func TestAttributeOnlyMapDoesNotRequireGroupResolution(t *testing.T) {
	doc := validServerUsers + `
authenticator_maps:
  - name: platform department
    order: 1
    when:
      attribute:
        name: department
        equals: platform
    map:
      type: organization
      organization: Engineering
      role: Organization Member
`
	cfg, err := ParseConfig([]byte(doc))
	if err != nil {
		t.Fatalf("attribute-only map should not require group_type: %v", err)
	}
	if cfg.UsesGroupMapping() {
		t.Fatal("attribute-only map unexpectedly requires group resolution")
	}
}
