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
