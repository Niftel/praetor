package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromotionTokenScopeComesFromCompatibilityManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "promote-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)

	required := []string{
		"id: release-repositories",
		"go run ./cmd/compatcheck -output repositories",
		"repositories: ${{ steps.release-repositories.outputs.repositories }}",
	}
	for _, fragment := range required {
		if !strings.Contains(workflow, fragment) {
			t.Errorf("promotion workflow is missing manifest-derived repository scope %q", fragment)
		}
	}

	if strings.Contains(workflow, "repositories: |\n") {
		t.Error("promotion workflow must not maintain a separate literal repository allowlist")
	}
}
