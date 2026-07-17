package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLDAPClientRejectsUnreadableCAFileBeforeDial(t *testing.T) {
	client := NewLDAPClient(&LDAPConfig{Server: LDAPServerConfig{
		URL:    "ldaps://ldap.invalid:636",
		CAFile: filepath.Join(t.TempDir(), "missing.pem"),
	}})
	if err := client.Connect(); err == nil || !strings.Contains(err.Error(), "read LDAP CA file") {
		t.Fatalf("Connect() error = %v, want LDAP CA read failure", err)
	}
}

func TestLDAPClientRejectsCAFileWithoutCertificates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewLDAPClient(&LDAPConfig{Server: LDAPServerConfig{
		URL:    "ldaps://ldap.invalid:636",
		CAFile: path,
	}})
	if err := client.Connect(); err == nil || !strings.Contains(err.Error(), "contains no PEM certificates") {
		t.Fatalf("Connect() error = %v, want invalid LDAP CA failure", err)
	}
}
