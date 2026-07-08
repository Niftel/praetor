package auth

import (
	"fmt"

	"github.com/go-ldap/ldap/v3"
)

// This file implements the real (LDAP-backed) GroupResolver: bind as the user to
// verify the password, then compute their group DN set per group_type. Unit tests
// use a fake GroupResolver instead, so nothing here needs a live server to test the
// mapper.

// group_type field accessors with defaults (defaults are also applied in
// config.applyDefaults, but guard here so direct callers are safe).
func (g LDAPGroupTypeConfig) filter() string {
	if g.SearchFilter != "" {
		return g.SearchFilter
	}
	return "(objectClass=groupOfNames)"
}

func (g LDAPGroupTypeConfig) memberAttr() string {
	if g.MemberAttribute != "" {
		return g.MemberAttribute
	}
	return "member"
}

func (g LDAPGroupTypeConfig) memberOfAttr() string {
	if g.MemberOfAttribute != "" {
		return g.MemberOfAttribute
	}
	return "memberOf"
}

// AuthenticateAndResolve implements GroupResolver. It manages its own connection
// lifecycle (connect → service bind → find user → user bind → service re-bind →
// resolve groups → close), so callers just invoke it per login.
func (c *LDAPClient) AuthenticateAndResolve(username, password string) (*UserIdentity, error) {
	if err := c.Connect(); err != nil {
		return nil, err
	}
	defer c.Close()
	if err := c.Bind(); err != nil {
		return nil, fmt.Errorf("service bind: %w", err)
	}

	entry, err := c.findUserEntry(username)
	if err != nil {
		return nil, err
	}

	// Verify the password by binding as the user's DN, then re-bind as the service
	// account for the group searches.
	if err := c.conn.Bind(entry.DN, password); err != nil {
		return nil, ErrInvalidCredentials
	}
	if err := c.Bind(); err != nil {
		return nil, fmt.Errorf("service re-bind: %w", err)
	}

	groups, err := c.resolveGroups(entry)
	if err != nil {
		return nil, err
	}

	id := &UserIdentity{
		DN:        entry.DN,
		Username:  entry.GetAttribute(c.config.Users.Attributes.Username),
		Email:     entry.GetAttribute(c.config.Users.Attributes.Email),
		FirstName: entry.GetAttribute(c.config.Users.Attributes.FirstName),
		LastName:  entry.GetAttribute(c.config.Users.Attributes.LastName),
		Groups:    groups,
	}
	if id.Username == "" {
		id.Username = username
	}
	if len(c.config.Users.Attributes.Custom) > 0 {
		id.Custom = make(map[string]string)
		for field, ldapAttr := range c.config.Users.Attributes.Custom {
			if v := entry.GetAttribute(ldapAttr); v != "" {
				id.Custom[field] = v
			}
		}
	}
	return id, nil
}

// findUserEntry locates exactly one user entry across the configured search bases.
// A not-found returns ErrInvalidCredentials (never leak whether the user exists).
func (c *LDAPClient) findUserEntry(username string) (*LDAPEntry, error) {
	filter := fmt.Sprintf("(&%s(%s=%s))",
		c.config.Users.SearchFilter,
		c.config.Users.Attributes.Username,
		ldap.EscapeFilter(username),
	)
	attrs := []string{
		c.config.Users.Attributes.Username,
		c.config.Users.Attributes.Email,
		c.config.Users.Attributes.FirstName,
		c.config.Users.Attributes.LastName,
		c.config.GroupType.memberOfAttr(),
	}
	for _, ldapAttr := range c.config.Users.Attributes.Custom {
		attrs = append(attrs, ldapAttr)
	}

	entries, err := c.searchMultipleBases(
		c.config.Users.GetSearchBases(),
		c.config.Users.SearchScope,
		filter,
		attrs,
	)
	if err != nil {
		return nil, fmt.Errorf("user search: %w", err)
	}
	if len(entries) == 0 {
		return nil, ErrInvalidCredentials
	}
	if len(entries) > 1 {
		return nil, fmt.Errorf("multiple users match %q", username)
	}
	return entries[0], nil
}

// resolveGroups computes the user's normalized group DN set per group_type.
func (c *LDAPClient) resolveGroups(entry *LDAPEntry) (map[string]struct{}, error) {
	gt := c.config.GroupType
	switch gt.Type {
	case GroupTypeMemberOf:
		return normalizeDNSet(entry.GetAttributes(gt.memberOfAttr())), nil
	case GroupTypePosix:
		uid := entry.GetAttribute(c.config.Users.Attributes.Username)
		raw, err := c.searchGroupRawDNs(fmt.Sprintf("(&%s(memberUid=%s))", gt.filter(), ldap.EscapeFilter(uid)))
		if err != nil {
			return nil, err
		}
		return normalizeDNSet(raw), nil
	case GroupTypeMemberDN:
		raw, err := c.searchGroupRawDNs(fmt.Sprintf("(&%s(%s=%s))", gt.filter(), gt.memberAttr(), ldap.EscapeFilter(entry.DN)))
		if err != nil {
			return nil, err
		}
		return normalizeDNSet(raw), nil
	case GroupTypeNested:
		return c.resolveNested(entry.DN)
	default:
		return nil, fmt.Errorf("unknown group_type %q", gt.Type)
	}
}

// searchGroupRawDNs returns the raw DNs of groups matching filter under the group
// search base (raw so nested traversal can search by the directory's own DN form).
func (c *LDAPClient) searchGroupRawDNs(filter string) ([]string, error) {
	entries, err := c.search(c.config.GroupType.SearchBase, SearchScopeSub, filter, []string{"1.1"})
	if err != nil {
		return nil, fmt.Errorf("group search: %w", err)
	}
	dns := make([]string, 0, len(entries))
	for _, e := range entries {
		dns = append(dns, e.DN)
	}
	return dns, nil
}

// resolveNested walks group→parent-group membership breadth-first up to max_depth,
// portable across AD and RFC groups (no reliance on the AD in-chain OID).
func (c *LDAPClient) resolveNested(userDN string) (map[string]struct{}, error) {
	gt := c.config.GroupType
	maxDepth := gt.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 5
	}
	result := make(map[string]struct{})
	frontier := []string{userDN}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, dn := range frontier {
			raw, err := c.searchGroupRawDNs(fmt.Sprintf("(&%s(%s=%s))", gt.filter(), gt.memberAttr(), ldap.EscapeFilter(dn)))
			if err != nil {
				return nil, err
			}
			for _, g := range raw {
				norm := NormalizeDN(g)
				if _, seen := result[norm]; !seen {
					result[norm] = struct{}{}
					next = append(next, g) // traverse by the directory's own DN form
				}
			}
		}
		frontier = next
	}
	return result, nil
}
