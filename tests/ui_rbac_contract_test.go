package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditorUIUsesEffectiveCapabilitiesAndFailsClosed(t *testing.T) {
	root := repositoryRoot(t)
	files := map[string][]string{
		filepath.Join(root, "web", "lib", "useCapabilities.ts"): {
			"const denied: ResourceCapabilities",
			"api.getCapabilities(contentType, objectId)",
			".catch(() => { if (active) setCapabilities(denied); })",
		},
		filepath.Join(root, "web", "pages", "WorkflowsPage.tsx"): {
			"orgCapabilities.add_workflow_template",
			"capabilities.manage",
			"capabilities.execute",
			"read only",
		},
		filepath.Join(root, "web", "pages", "WorkflowBuilderPage.tsx"): {
			"workflowCapabilities.manage",
			"Read-only access",
		},
		filepath.Join(root, "web", "pages", "InventoriesPage.tsx"): {
			"orgCapabilities.add_inventory",
			"inventoryCapabilities.manage",
			"canManage={canManageInventory}",
			"readOnly={!canManageInventory}",
		},
	}
	for path, required := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, contract := range required {
			if !strings.Contains(string(raw), contract) {
				t.Errorf("%s must contain RBAC contract %q", filepath.Base(path), contract)
			}
		}
	}
}
