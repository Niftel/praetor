package auth

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxPredicateDepth      = 8
	maxPredicateConditions = 64
	maxPredicateValueBytes = 1024
)

// AuthenticatorMap is a provider-neutral, ordered authorization rule. LDAP is
// currently the first identity provider feeding it, but predicates consume only
// normalized identity claims and contain no LDAP queries or filters.
type AuthenticatorMap struct {
	Name   string             `yaml:"name" json:"name"`
	Order  int                `yaml:"order" json:"order"`
	When   Predicate          `yaml:"when" json:"when"`
	Map    AuthenticatorGrant `yaml:"map" json:"map"`
	Revoke bool               `yaml:"revoke" json:"revoke"`
}

// Predicate is an explicit expression tree. Exactly one operator must be set
// per node, avoiding implicit AND/OR precedence. Leaf comparisons are exact;
// regex and arbitrary provider queries are intentionally unsupported.
type Predicate struct {
	All       []Predicate         `yaml:"all,omitempty" json:"all,omitempty"`
	Any       []Predicate         `yaml:"any,omitempty" json:"any,omitempty"`
	Not       *Predicate          `yaml:"not,omitempty" json:"not,omitempty"`
	Group     string              `yaml:"group,omitempty" json:"group,omitempty"`
	Attribute *AttributePredicate `yaml:"attribute,omitempty" json:"attribute,omitempty"`
	Always    *bool               `yaml:"always,omitempty" json:"always,omitempty"`
}

type AttributePredicate struct {
	Name   string `yaml:"name" json:"name"`
	Equals string `yaml:"equals" json:"equals"`
}

// AuthenticatorGrant deliberately targets platform identity constructs, not
// arbitrary controller resources. Teams receive resource roles through normal
// RBAC assignments after authentication.
type AuthenticatorGrant struct {
	Type         string `yaml:"type" json:"type"`
	Organization string `yaml:"organization,omitempty" json:"organization,omitempty"`
	Team         string `yaml:"team,omitempty" json:"team,omitempty"`
	Role         string `yaml:"role,omitempty" json:"role,omitempty"`
}

const (
	MapAllow        = "allow"
	MapOrganization = "organization"
	MapTeam         = "team"
	MapRole         = "role"
	MapSuperuser    = "is_superuser"
)

// IdentityClaims is the normalized input to every authenticator rule.
type IdentityClaims struct {
	Groups     map[string]struct{}
	Attributes map[string][]string
}

func (id *UserIdentity) Claims() IdentityClaims {
	attrs := map[string][]string{
		"username":   {id.Username},
		"email":      {id.Email},
		"first_name": {id.FirstName},
		"last_name":  {id.LastName},
	}
	for name, value := range id.Custom {
		attrs[name] = []string{value}
	}
	return IdentityClaims{Groups: id.Groups, Attributes: attrs}
}

func ValidateAuthenticatorMaps(maps []AuthenticatorMap) error {
	seenNames := map[string]struct{}{}
	seenOrder := map[int]struct{}{}
	var errs []string
	for i := range maps {
		m := &maps[i]
		prefix := fmt.Sprintf("authenticator_maps[%d]", i)
		if strings.TrimSpace(m.Name) == "" {
			errs = append(errs, prefix+".name is required")
		} else if _, exists := seenNames[m.Name]; exists {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", prefix, m.Name))
		} else {
			seenNames[m.Name] = struct{}{}
		}
		if m.Order < 0 {
			errs = append(errs, prefix+".order must be zero or greater")
		} else if _, exists := seenOrder[m.Order]; exists {
			errs = append(errs, fmt.Sprintf("%s.order %d is duplicated", prefix, m.Order))
		} else {
			seenOrder[m.Order] = struct{}{}
		}
		count := 0
		if err := validatePredicate(m.When, 1, &count); err != nil {
			errs = append(errs, prefix+".when: "+err.Error())
		}
		if err := validateGrant(m.Map); err != nil {
			errs = append(errs, prefix+".map: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func validatePredicate(p Predicate, depth int, count *int) error {
	if depth > maxPredicateDepth {
		return fmt.Errorf("maximum nesting depth is %d", maxPredicateDepth)
	}
	(*count)++
	if *count > maxPredicateConditions {
		return fmt.Errorf("maximum condition count is %d", maxPredicateConditions)
	}
	operators := 0
	if p.All != nil {
		operators++
	}
	if p.Any != nil {
		operators++
	}
	if p.Not != nil {
		operators++
	}
	if p.Group != "" {
		operators++
	}
	if p.Attribute != nil {
		operators++
	}
	if p.Always != nil {
		operators++
	}
	if operators != 1 {
		return fmt.Errorf("exactly one of all, any, not, group, attribute, or always is required")
	}
	if p.All != nil {
		if len(p.All) == 0 {
			return fmt.Errorf("all requires at least one child")
		}
		for _, child := range p.All {
			if err := validatePredicate(child, depth+1, count); err != nil {
				return err
			}
		}
	}
	if p.Any != nil {
		if len(p.Any) == 0 {
			return fmt.Errorf("any requires at least one child")
		}
		for _, child := range p.Any {
			if err := validatePredicate(child, depth+1, count); err != nil {
				return err
			}
		}
	}
	if p.Not != nil {
		if err := validatePredicate(*p.Not, depth+1, count); err != nil {
			return err
		}
	}
	if p.Group != "" && len(p.Group) > maxPredicateValueBytes {
		return fmt.Errorf("group value exceeds %d bytes", maxPredicateValueBytes)
	}
	if p.Attribute != nil {
		if strings.TrimSpace(p.Attribute.Name) == "" {
			return fmt.Errorf("attribute.name is required")
		}
		if p.Attribute.Equals == "" {
			return fmt.Errorf("attribute.equals is required")
		}
		if len(p.Attribute.Name) > maxPredicateValueBytes || len(p.Attribute.Equals) > maxPredicateValueBytes {
			return fmt.Errorf("attribute value exceeds %d bytes", maxPredicateValueBytes)
		}
	}
	return nil
}

func validateGrant(g AuthenticatorGrant) error {
	if len(g.Organization) > maxPredicateValueBytes || len(g.Team) > maxPredicateValueBytes || len(g.Role) > maxPredicateValueBytes {
		return fmt.Errorf("target value exceeds %d bytes", maxPredicateValueBytes)
	}
	switch g.Type {
	case MapAllow, MapSuperuser:
		if g.Organization != "" || g.Team != "" || g.Role != "" {
			return fmt.Errorf("%s mapping accepts no target fields", g.Type)
		}
	case MapOrganization:
		if g.Organization == "" {
			return fmt.Errorf("organization is required")
		}
		if g.Role != "Organization Admin" && g.Role != "Organization Member" && g.Role != "Organization Auditor" {
			return fmt.Errorf("role must be Organization Admin, Organization Member, or Organization Auditor")
		}
		if g.Team != "" {
			return fmt.Errorf("team is not valid for an organization mapping")
		}
	case MapTeam:
		if g.Organization == "" || g.Team == "" {
			return fmt.Errorf("organization and team are required")
		}
		if g.Role != "Team Admin" && g.Role != "Team Member" {
			return fmt.Errorf("role must be Team Admin or Team Member")
		}
	case MapRole:
		if g.Role != "System Auditor" {
			return fmt.Errorf("only the global System Auditor role is supported")
		}
		if g.Organization != "" || g.Team != "" {
			return fmt.Errorf("global role mappings cannot target an organization or team")
		}
	default:
		return fmt.Errorf("type must be allow, organization, team, role, or is_superuser")
	}
	return nil
}

func (p Predicate) Matches(claims IdentityClaims) bool {
	switch {
	case p.All != nil:
		for _, child := range p.All {
			if !child.Matches(claims) {
				return false
			}
		}
		return true
	case p.Any != nil:
		for _, child := range p.Any {
			if child.Matches(claims) {
				return true
			}
		}
		return false
	case p.Not != nil:
		return !p.Not.Matches(claims)
	case p.Group != "":
		_, ok := claims.Groups[NormalizeDN(p.Group)]
		return ok
	case p.Attribute != nil:
		values, ok := claims.Attributes[p.Attribute.Name]
		if !ok {
			return false
		}
		for _, value := range values {
			if value == p.Attribute.Equals {
				return true
			}
		}
		return false
	case p.Always != nil:
		return *p.Always
	default:
		return false // malformed/unvalidated predicates fail closed
	}
}

func predicateUsesGroup(p Predicate) bool {
	if p.Group != "" {
		return true
	}
	if p.Not != nil && predicateUsesGroup(*p.Not) {
		return true
	}
	for _, child := range p.All {
		if predicateUsesGroup(child) {
			return true
		}
	}
	for _, child := range p.Any {
		if predicateUsesGroup(child) {
			return true
		}
	}
	return false
}

func sortedAuthenticatorMaps(maps []AuthenticatorMap) []AuthenticatorMap {
	out := append([]AuthenticatorMap(nil), maps...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}
