package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praetordev/praetor/pkg/events"
)

func TestGalaxyEnv(t *testing.T) {
	if got := galaxyEnv(nil); got != nil {
		t.Fatalf("no servers should yield nil env, got %v", got)
	}

	env := galaxyEnv([]events.GalaxyServer{
		{Name: "automation_hub", URL: "https://hub.example/api/", Token: "t0ken", AuthURL: "https://sso.example/token"},
		{Name: "released", URL: "https://galaxy.ansible.com/"},
		{Name: "skipme"}, // no URL -> dropped
	})
	joined := strings.Join(env, "\n")

	for _, want := range []string{
		"ANSIBLE_GALAXY_SERVER_AUTOMATION_HUB_URL=https://hub.example/api/",
		"ANSIBLE_GALAXY_SERVER_AUTOMATION_HUB_TOKEN=t0ken",
		"ANSIBLE_GALAXY_SERVER_AUTOMATION_HUB_AUTH_URL=https://sso.example/token",
		"ANSIBLE_GALAXY_SERVER_RELEASED_URL=https://galaxy.ansible.com/",
		"ANSIBLE_GALAXY_SERVER_LIST=automation_hub,released",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "SKIPME") {
		t.Error("server without a URL should be dropped")
	}
}

func TestFirstExisting(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "collections"), 0755)
	os.WriteFile(filepath.Join(dir, "collections", "requirements.yml"), []byte("collections: []"), 0644)
	os.WriteFile(filepath.Join(dir, "requirements.yml"), []byte("- name: x"), 0644)

	if got := firstExisting(dir, "collections/requirements.yml"); got != filepath.Join(dir, "collections/requirements.yml") {
		t.Fatalf("collections requirements not found: %q", got)
	}
	// roles preference: roles/requirements.yml absent, falls back to bare requirements.yml
	if got := firstExisting(dir, "roles/requirements.yml", "requirements.yml"); got != filepath.Join(dir, "requirements.yml") {
		t.Fatalf("expected fallback to bare requirements.yml, got %q", got)
	}
	if got := firstExisting(dir, "nope/requirements.yml"); got != "" {
		t.Fatalf("missing file should yield empty, got %q", got)
	}
}
