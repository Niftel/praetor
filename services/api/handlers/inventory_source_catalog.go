package handlers

import (
	"net/http"

	"github.com/praetordev/praetor/pkg/inventorysourcecatalog"
	"github.com/praetordev/render"
)

type InventorySourceCatalogResponse struct {
	Version string                              `json:"version"`
	Results []inventorysourcecatalog.SourceType `json:"results"`
}

// ListInventorySourceTypes GET /api/v1/inventory-source-types
// Catalog metadata is server-owned and contains no credential instances or
// values. Authentication is still provided by the enclosing API router.
func ListInventorySourceTypes(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, InventorySourceCatalogResponse{
		Version: inventorysourcecatalog.Version,
		Results: inventorysourcecatalog.List(),
	})
}
