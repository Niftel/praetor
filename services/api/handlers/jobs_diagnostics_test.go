package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestParseDiagnosticQuery(t *testing.T) {
	request := httptest.NewRequest("GET", "/?cursor=41&limit=25&kind=host&outcome=failed", nil)
	query, err := parseDiagnosticQuery(request)
	if err != nil {
		t.Fatal(err)
	}
	if query.AfterSeq != 41 || query.Limit != 25 || query.Kind != "host" || query.Outcome != "failed" {
		t.Fatalf("unexpected query: %#v", query)
	}

	for _, target := range []string{
		"/?cursor=-1", "/?limit=0", "/?limit=201", "/?kind=secret", "/?outcome=unknown",
	} {
		request := httptest.NewRequest("GET", target, nil)
		if _, err := parseDiagnosticQuery(request); err == nil {
			t.Fatalf("expected %s to be rejected", target)
		}
	}
}
