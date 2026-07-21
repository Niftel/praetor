package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseInventorySyncHistoryFilter(t *testing.T) {
	req := httptest.NewRequest("GET", "/history?status=failed&phase=reconciliation&limit=500", nil)
	filter, err := parseInventorySyncHistoryFilter(req)
	if err != nil {
		t.Fatal(err)
	}
	if filter.Status != "failed" || filter.Phase != "reconciliation" || filter.Limit != 100 {
		t.Fatalf("filter = %#v", filter)
	}
	for _, query := range []string{"status=unknown", "phase=secret-resolution", "limit=zero", "limit=0"} {
		req := httptest.NewRequest("GET", "/history?"+query, nil)
		if _, err := parseInventorySyncHistoryFilter(req); err == nil {
			t.Errorf("query %q unexpectedly passed", query)
		}
	}
}

func TestRedactDiagnosticDetailsNestedProviderSecrets(t *testing.T) {
	raw := json.RawMessage(`{
		"provider":"aws",
		"request":{"authorization":"Bearer abc","headers":{"Cookie":"session=abc"}},
		"errors":[{"message":"denied","context":{"access-token":"abc","region":"eu-west-1"}}],
		"credentials":{"username":"operator","password":"secret"}
	}`)
	redacted := redactDiagnosticDetails(raw)
	text := string(redacted)
	for _, secret := range []string{"Bearer abc", "session=abc", `"abc"`, "secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("redacted diagnostic still contains %q: %s", secret, text)
		}
	}
	for _, safe := range []string{"aws", "denied", "eu-west-1"} {
		if !strings.Contains(text, safe) {
			t.Fatalf("redaction removed safe diagnostic %q: %s", safe, text)
		}
	}
}

func TestRedactDiagnosticDetailsRejectsMalformedPayload(t *testing.T) {
	if got := string(redactDiagnosticDetails(json.RawMessage(`{"token":`))); got != `{}` {
		t.Fatalf("malformed diagnostic = %s, want {}", got)
	}
}
