package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/praetordev/praetor/pkg/inventorysourcecatalog"
)

func TestListInventorySourceTypes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inventory-source-types", nil)
	rec := httptest.NewRecorder()
	ListInventorySourceTypes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	payload := rec.Body.Bytes()
	var response InventorySourceCatalogResponse
	if err := json.NewDecoder(bytes.NewReader(payload)).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Version != inventorysourcecatalog.Version || len(response.Results) != 5 {
		t.Fatalf("response = %#v", response)
	}
	encoded := string(payload)
	for _, forbidden := range []string{"secret_key", "client_secret", "service_account_content"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("catalog exposed forbidden credential field %q", forbidden)
		}
	}
}
