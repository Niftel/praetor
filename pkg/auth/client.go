package auth

import (
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// LDAPClient wraps the go-ldap client with convenience methods.
type LDAPClient struct {
	config *LDAPConfig
	conn   *ldap.Conn
}

// NewLDAPClient creates a new LDAP client from the given configuration.
func NewLDAPClient(config *LDAPConfig) *LDAPClient {
	return &LDAPClient{
		config: config,
	}
}

// Connect establishes a connection to the LDAP server.
func (c *LDAPClient) Connect() error {
	var conn *ldap.Conn
	var err error

	// Determine if using TLS
	url := c.config.Server.URL
	useTLS := strings.HasPrefix(url, "ldaps://")

	// Build TLS config
	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.config.Server.InsecureSkipVerify,
	}

	// Set timeout
	ldap.DefaultTimeout = c.config.Server.Timeout

	if useTLS {
		// LDAPS connection
		conn, err = ldap.DialURL(url, ldap.DialWithTLSConfig(tlsConfig))
	} else {
		// Plain LDAP connection
		conn, err = ldap.DialURL(url)
		if err == nil && c.config.Server.StartTLS {
			// Upgrade to TLS
			err = conn.StartTLS(tlsConfig)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to connect to LDAP server: %w", err)
	}

	c.conn = conn
	return nil
}

// Bind authenticates with the LDAP server using service account credentials.
func (c *LDAPClient) Bind() error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	err := c.conn.Bind(c.config.Server.BindDN, c.config.Server.BindPassword)
	if err != nil {
		return fmt.Errorf("failed to bind: %w", err)
	}

	return nil
}

// Close closes the LDAP connection.
func (c *LDAPClient) Close() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// SearchUsers searches for users in LDAP based on configuration.
// Iterates through all configured search bases.
func (c *LDAPClient) SearchUsers() ([]*LDAPEntry, error) {
	attrs := []string{
		c.config.Users.Attributes.Username,
		c.config.Users.Attributes.Email,
		c.config.Users.Attributes.FirstName,
		c.config.Users.Attributes.LastName,
	}
	// Add custom attribute names to fetch
	for _, ldapAttr := range c.config.Users.Attributes.Custom {
		attrs = append(attrs, ldapAttr)
	}

	return c.searchMultipleBases(
		c.config.Users.GetSearchBases(),
		c.config.Users.SearchScope,
		c.config.Users.SearchFilter,
		attrs,
	)
}

// SearchOrganizations searches for organizations in LDAP based on configuration.
// Iterates through all configured search bases.
func (c *LDAPClient) SearchOrganizations() ([]*LDAPEntry, error) {
	if !c.config.Organizations.Enabled {
		return nil, nil
	}

	attrs := []string{
		c.config.Organizations.Attributes.Name,
		c.config.Organizations.Attributes.Description,
		c.config.Organizations.MemberAttribute,
	}

	return c.searchMultipleBases(
		c.config.Organizations.GetSearchBases(),
		c.config.Organizations.SearchScope,
		c.config.Organizations.SearchFilter,
		attrs,
	)
}

// SearchTeams searches for teams in LDAP based on configuration.
// Iterates through all configured search bases.
func (c *LDAPClient) SearchTeams() ([]*LDAPEntry, error) {
	if !c.config.Teams.Enabled {
		return nil, nil
	}

	attrs := []string{
		c.config.Teams.Attributes.Name,
		c.config.Teams.Attributes.Description,
		c.config.Teams.MemberAttribute,
		c.config.Teams.OrganizationAttribute,
	}

	return c.searchMultipleBases(
		c.config.Teams.GetSearchBases(),
		c.config.Teams.SearchScope,
		c.config.Teams.SearchFilter,
		attrs,
	)
}

// AuthenticateUser attempts to authenticate a user with the given credentials.
// Returns the user's LDAP entry if successful.
func (c *LDAPClient) AuthenticateUser(username, password string) (*LDAPEntry, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	// First, search for the user to get their DN
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
	}

	entries, err := c.search(
		c.config.Users.SearchBase,
		c.config.Users.SearchScope,
		filter,
		attrs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search for user: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("user not found")
	}

	if len(entries) > 1 {
		return nil, fmt.Errorf("multiple users found with username %q", username)
	}

	entry := entries[0]

	// Try to bind as the user to verify password
	err = c.conn.Bind(entry.DN, password)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Re-bind as service account for further operations
	err = c.Bind()
	if err != nil {
		return nil, fmt.Errorf("failed to re-bind as service account: %w", err)
	}

	return entry, nil
}

// TestConnection tests the LDAP connection and bind.
func (c *LDAPClient) TestConnection() error {
	if err := c.Connect(); err != nil {
		return err
	}
	defer c.Close()

	if err := c.Bind(); err != nil {
		return err
	}

	return nil
}

// searchMultipleBases searches across multiple base DNs and aggregates results.
func (c *LDAPClient) searchMultipleBases(baseDNs []string, scope LDAPSearchScope, filter string, attributes []string) ([]*LDAPEntry, error) {
	if len(baseDNs) == 0 {
		return nil, fmt.Errorf("no search bases configured")
	}

	var allEntries []*LDAPEntry
	for _, baseDN := range baseDNs {
		entries, err := c.search(baseDN, scope, filter, attributes)
		if err != nil {
			// Log but continue with other bases
			continue
		}
		allEntries = append(allEntries, entries...)
	}

	return allEntries, nil
}

// search performs an LDAP search and returns entries.
func (c *LDAPClient) search(baseDN string, scope LDAPSearchScope, filter string, attributes []string) ([]*LDAPEntry, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	// Filter out empty attributes
	var nonEmptyAttrs []string
	for _, attr := range attributes {
		if attr != "" {
			nonEmptyAttrs = append(nonEmptyAttrs, attr)
		}
	}

	searchRequest := ldap.NewSearchRequest(
		baseDN,
		scopeToInt(scope),
		ldap.NeverDerefAliases,
		0, // Size limit (0 = unlimited)
		0, // Time limit (0 = unlimited)
		false,
		filter,
		nonEmptyAttrs,
		nil,
	)

	result, err := c.conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	entries := make([]*LDAPEntry, 0, len(result.Entries))
	for _, e := range result.Entries {
		entry := &LDAPEntry{
			DN:         e.DN,
			Attributes: make(map[string][]string),
		}
		for _, attr := range e.Attributes {
			entry.Attributes[attr.Name] = attr.Values
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// scopeToInt converts LDAPSearchScope to go-ldap scope constant.
func scopeToInt(scope LDAPSearchScope) int {
	switch scope {
	case SearchScopeBase:
		return ldap.ScopeBaseObject
	case SearchScopeOne:
		return ldap.ScopeSingleLevel
	case SearchScopeSub:
		return ldap.ScopeWholeSubtree
	default:
		return ldap.ScopeWholeSubtree
	}
}
