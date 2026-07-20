package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDependabotDoesNotBundleMajorWebMigrations(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "dependabot.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("dependabot config must remain valid YAML: %v", err)
	}
	config := string(raw)
	webStart := strings.Index(config, "directory: /web")
	docsStart := strings.Index(config, "directory: /docs-site")
	if webStart < 0 || docsStart <= webStart {
		t.Fatal("dependabot config must retain separate web and docs npm entries")
	}
	webConfig := config[webStart:docsStart]
	for _, required := range []string{
		"web-dependencies:",
		`patterns: ["*"]`,
		`update-types: ["minor", "patch"]`,
	} {
		if !strings.Contains(webConfig, required) {
			t.Fatalf("web dependency policy must contain %q", required)
		}
	}
	if strings.Contains(webConfig, `update-types: ["major"`) || strings.Contains(webConfig, `"major"]`) {
		t.Fatal("major web migrations must not be included in the grouped update policy")
	}
}
