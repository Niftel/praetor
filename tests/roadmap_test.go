package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRoadmapReflectsShippedCapabilities(t *testing.T) {
	roadmap := readRoadmap(t)
	required := []string{
		"Prompt-on-launch and surveys",
		"Notifications and inbound webhooks",
		"Dynamic inventory, fact caching, and launch scope",
		"Workflow DAGs and approvals",
		"Praetor Secrets integration",
		"Delegated API launches",
		"Platform release automation",
		"Shared-module release health",
		"There is no committed roadmap item",
	}
	for _, text := range required {
		if !strings.Contains(roadmap, text) {
			t.Errorf("roadmap is missing current capability or committed work %q", text)
		}
	}

	stale := []string{
		"Praetor Roadmap — Missing Features",
		"the roles exist; the features do not",
		"## Phase 1 — Launch-time inputs",
		"## Phase 4 — Workflows",
	}
	for _, text := range stale {
		if strings.Contains(roadmap, text) {
			t.Errorf("roadmap still describes shipped work as missing: %q", text)
		}
	}
}

func TestRoadmapEvidenceExists(t *testing.T) {
	root := filepath.Join("..")
	evidence := []string{
		"platform-compatibility.yaml",
		"db/migrations/000020_prompt_on_launch.up.sql",
		"db/migrations/000021_surveys.up.sql",
		"db/migrations/000022_notifications.up.sql",
		"db/migrations/000023_webhooks.up.sql",
		"db/migrations/000024_fact_cache.up.sql",
		"db/migrations/000026_inventory_sources.up.sql",
		"db/migrations/000027_workflows.up.sql",
		"db/migrations/000028_activity_stream.up.sql",
		"db/migrations/000063_secrets_service_credential_reference.up.sql",
		"db/migrations/000066_service_principals.up.sql",
		"db/migrations/000067_delegated_launch_grants.up.sql",
		"db/migrations/000068_delegated_workflow_launches.up.sql",
		".github/workflows/promote-release.yml",
		".github/workflows/release-preflight.yml",
		"cmd/host-runner/galaxy.go",
		"services/api/router.go",
		"services/api/handlers/delegated_launch.go",
		"services/api/handlers/workflows.go",
	}
	for _, name := range evidence {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("roadmap evidence %s is missing: %v", name, err)
		}
	}
}

func TestRoadmapLocalLinksResolve(t *testing.T) {
	roadmap := readRoadmap(t)
	link := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	for _, match := range link.FindAllStringSubmatch(roadmap, -1) {
		target := strings.SplitN(match[1], "#", 2)[0]
		if strings.Contains(target, "://") {
			continue
		}
		if _, err := os.Stat(filepath.Join("..", filepath.FromSlash(target))); err != nil {
			t.Errorf("roadmap link %q does not resolve: %v", match[1], err)
		}
	}
}

func readRoadmap(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "ROADMAP.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
