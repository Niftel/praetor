package auth

import (
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
	"gopkg.in/yaml.v3"
)

// This file implements the AAP/AWX login-time group→role mapping model
// (see pkg/auth/LDAP-REDESIGN.md). LDAP groups are bound to Praetor roles by the
// operator; membership is evaluated at login. Nothing here does directory I/O —
// it is pure config + decision logic, unit-tested without a live LDAP server.

// LDAPGroupTypeConfig controls how a user's group memberships are computed.
type LDAPGroupTypeConfig struct {
	// Type ∈ {member_dn, member_of, posix, nested}. member_dn searches groups whose
	// member attribute contains the user DN; member_of reads the user's memberOf
	// attribute; posix matches memberUid; nested resolves transitive membership.
	Type              string `yaml:"type"`
	SearchBase        string `yaml:"search_base"`         // group search base (member_dn/posix/nested)
	SearchFilter      string `yaml:"search_filter"`       // default (objectClass=groupOfNames)
	MemberAttribute   string `yaml:"member_attribute"`    // default "member" (posix: "memberUid")
	MemberOfAttribute string `yaml:"member_of_attribute"` // default "memberOf"
	MaxDepth          int    `yaml:"max_depth"`           // nested only, default 5
}

const (
	GroupTypeMemberDN = "member_dn"
	GroupTypeMemberOf = "member_of"
	GroupTypePosix    = "posix"
	GroupTypeNested   = "nested"
)

// LDAPUserFlagsConfig maps LDAP group membership to Praetor user flags.
type LDAPUserFlagsConfig struct {
	IsSuperuser     GroupDNList `yaml:"is_superuser"`
	IsSystemAuditor GroupDNList `yaml:"is_system_auditor"`
}

// LDAPOrgMapEntry binds LDAP groups to organization roles. Keyed by org name in
// LDAPConfig.OrganizationMap; the org is created if absent.
type LDAPOrgMapEntry struct {
	Admins         GroupMatch `yaml:"admins"`
	Users          GroupMatch `yaml:"users"`
	Auditors       GroupMatch `yaml:"auditors"`
	RemoveAdmins   bool       `yaml:"remove_admins"`
	RemoveUsers    bool       `yaml:"remove_users"`
	RemoveAuditors bool       `yaml:"remove_auditors"`

	// Roles maps a DAB RoleDefinition NAME (managed or custom, e.g. "Organization
	// Auditor" or a bespoke "Engineer Audit") to the LDAP group that grants it,
	// scoped to this organization (Gitea #98). This is what lets an operator bind a
	// directory group to an arbitrary capability bundle, not just the fixed
	// admin/member/auditor triple. An unknown role name is a hard config error.
	Roles map[string]GroupMatch `yaml:"roles"`
	// RemoveRoles revokes a Roles entry when the user is no longer in its group
	// (same semantics as remove_admins/users/auditors).
	RemoveRoles bool `yaml:"remove_roles"`
}

// LDAPTeamMapEntry binds an LDAP group to a team's membership. Keyed by team name
// in LDAPConfig.TeamMap.
type LDAPTeamMapEntry struct {
	Organization string     `yaml:"organization"` // required; created if absent
	Users        GroupMatch `yaml:"users"`
	Remove       bool       `yaml:"remove"`
}

// GroupMatch mirrors django-auth-ldap's DN-string / list-of-DNs / bool trichotomy.
// In YAML: `true`/`false` (all/none), a single DN string, or a list of DN strings.
type GroupMatch struct {
	All *bool    // set when the YAML value was a bool
	DNs []string // one or more group DNs
}

// UnmarshalYAML accepts a bool, a scalar string, or a sequence of strings.
func (m *GroupMatch) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		// Try bool first (true/false → All); otherwise treat as a single DN.
		var b bool
		if err := value.Decode(&b); err == nil {
			m.All = &b
			return nil
		}
		var s string
		if err := value.Decode(&s); err != nil {
			return fmt.Errorf("group match: %w", err)
		}
		if s != "" {
			m.DNs = []string{s}
		}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := value.Decode(&list); err != nil {
			return fmt.Errorf("group match list: %w", err)
		}
		m.DNs = list
		return nil
	default:
		return fmt.Errorf("group match must be a bool, string, or list of strings")
	}
}

// Configured reports whether the operator set this match at all (needed for
// remove_* semantics: an unconfigured match never revokes).
func (m GroupMatch) Configured() bool { return m.All != nil || len(m.DNs) > 0 }

// Matches reports whether a user with the given (normalized) group DN set matches.
func (m GroupMatch) Matches(groups map[string]struct{}) bool {
	if m.All != nil {
		return *m.All
	}
	for _, dn := range m.DNs {
		if _, ok := groups[NormalizeDN(dn)]; ok {
			return true
		}
	}
	return false
}

// GroupDNList is like GroupMatch without the bool form — a DN or list of DNs used
// by user_flags_by_group.
type GroupDNList struct {
	DNs []string
}

func (l *GroupDNList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var s string
		if err := value.Decode(&s); err != nil {
			return fmt.Errorf("group dn: %w", err)
		}
		if s != "" {
			l.DNs = []string{s}
		}
		return nil
	case yaml.SequenceNode:
		return value.Decode(&l.DNs)
	default:
		return fmt.Errorf("group dn list must be a string or list of strings")
	}
}

// Configured reports whether the flag mapping is set. When it is NOT configured the
// flag must be left untouched at login (unset ≠ false) — this protects a manually
// promoted superuser from being demoted on every login.
func (l GroupDNList) Configured() bool { return len(l.DNs) > 0 }

// Resolve returns (value, assign): assign is false when the mapping is unconfigured,
// in which case the caller must not write the flag.
func (l GroupDNList) Resolve(groups map[string]struct{}) (value bool, assign bool) {
	if !l.Configured() {
		return false, false
	}
	for _, dn := range l.DNs {
		if _, ok := groups[NormalizeDN(dn)]; ok {
			return true, true
		}
	}
	return false, true
}

// decideRole returns whether to grant and/or revoke a role for a user, given the
// match result, whether the mapping is configured, and the entry's remove flag.
// django-auth-ldap parity: match ⇒ grant; no match ⇒ revoke only when configured
// AND remove is set (grant-only otherwise).
func decideRole(matched, configured, remove bool) (grant, revoke bool) {
	if matched {
		return true, false
	}
	if configured && remove {
		return false, true
	}
	return false, false
}

// NormalizeDN canonicalizes a DN (lower-cased type=value RDNs, trimmed) so config
// DNs and directory-returned DNs compare equal despite case/spacing differences.
// Falls back to a trimmed lower-case string if the DN can't be parsed.
func NormalizeDN(dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(dn))
	}
	parts := make([]string, 0, len(parsed.RDNs))
	for _, rdn := range parsed.RDNs {
		for _, ava := range rdn.Attributes {
			parts = append(parts, strings.ToLower(strings.TrimSpace(ava.Type))+"="+strings.ToLower(strings.TrimSpace(ava.Value)))
		}
	}
	return strings.Join(parts, ",")
}

// normalizeDNSet normalizes every DN in a slice into a set for membership tests.
func normalizeDNSet(dns []string) map[string]struct{} {
	set := make(map[string]struct{}, len(dns))
	for _, dn := range dns {
		if dn == "" {
			continue
		}
		set[NormalizeDN(dn)] = struct{}{}
	}
	return set
}
