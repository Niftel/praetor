package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/praetordev/events"
)

func TestScrubManifestSecretsPreservesReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	canary := "manifest-secret-canary"
	request := events.ExecutionRequest{
		ManifestVersion: 1,
		ExecutionRunID:  uuid.New(),
		UnifiedJobID:    42,
		JobManifest: events.JobManifest{
			CredentialID:    7,
			CredentialEnv:   map[string]string{"ANSIBLE_REMOTE_USER": canary},
			CredentialFiles: map[string]string{"ANSIBLE_PASSWORD_FILE": canary},
			SSHPrivateKey:   canary,
			IngestToken:     canary,
			GalaxyServers:   []events.GalaxyServer{{Name: "hub", URL: "https://hub.example", Token: canary}},
		},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := scrubManifestSecrets(path); err != nil {
		t.Fatal(err)
	}
	scrubbed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(scrubbed), canary) {
		t.Fatalf("terminal manifest retained secret material: %s", scrubbed)
	}
	var decoded events.ExecutionRequest
	if err := json.Unmarshal(scrubbed, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ExecutionRunID != request.ExecutionRunID || decoded.UnifiedJobID != 42 || decoded.JobManifest.CredentialID != 7 {
		t.Fatalf("scrub changed non-secret run references: %+v", decoded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("scrubbed manifest mode=%v", info.Mode().Perm())
	}
}

func TestMaterializeCredentialFilesUsesPrivateTemporaryPaths(t *testing.T) {
	jobDir := t.TempDir()
	canary := "temporary-secret-canary"
	environment, cleanup, err := materializeCredentialFiles(jobDir, map[string]string{
		"ANSIBLE_PASSWORD_FILE": canary,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := environment["ANSIBLE_PASSWORD_FILE"]
	content, err := os.ReadFile(path)
	if err != nil || string(content) != canary {
		t.Fatalf("materialized content=%q err=%v", content, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode=%v", info.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(filepath.Join(jobDir, ".credentials")); !os.IsNotExist(err) {
		t.Fatalf("credential directory survived cleanup: %v", err)
	}
}

func TestMaterializeCredentialFilesRejectsEnvironmentInjection(t *testing.T) {
	_, _, err := materializeCredentialFiles(t.TempDir(), map[string]string{"BAD-NAME": "secret"})
	if err == nil {
		t.Fatal("invalid environment name was accepted")
	}
}
