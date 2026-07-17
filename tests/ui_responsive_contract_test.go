package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNarrowViewportUILayoutContracts(t *testing.T) {
	root := repoRoot(t)
	cases := map[string][]string{
		filepath.Join(root, "web", "components", "Shell.tsx"): {
			"max-[700px]:w-auto",
			"max-[520px]:hidden",
		},
		filepath.Join(root, "web", "pages", "WorkflowsPage.tsx"): {
			"max-[640px]:px-4",
			"max-[520px]:col-span-3",
			"grid-cols-[3.5rem_auto_minmax(0,1fr)]",
		},
		filepath.Join(root, "web", "pages", "InventoriesPage.tsx"): {
			"mobilePane",
			"Hosts & groups",
			"max-[820px]:grid-rows-[44px_minmax(0,1fr)]",
			"max-[520px]:grid-cols-1",
		},
	}

	for path, markers := range cases {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(body), marker) {
				t.Errorf("%s must retain responsive contract %q", filepath.Base(path), marker)
			}
		}
	}
}
