package events

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// updateGolden regenerates the golden files instead of asserting against them.
// Run: go test ./pkg/events -run Golden -update   (or UPDATE_GOLDEN=1 go test ...)
var updateGolden = flag.Bool("update", false, "update golden files")

func wantUpdate() bool { return *updateGolden || os.Getenv("UPDATE_GOLDEN") == "1" }

// fullyPopulatedRequest builds an ExecutionRequest with every field of the
// wire contract set to a fixed, recognisable value. It is deliberately
// exhaustive — including the executor-filled fields (CredentialEnv, Inventory,
// CachedFacts, IngestToken) that are normally absent from the outbox/NATS
// message — so the golden pins the *entire* shape the host-runner may see.
//
// This is a guard rail for the launch-pipeline decomposition (B1): the
// host-runner rides its own release train and reads this struct on target
// hosts, so any accidental rename, retag, or removal of a field is a
// cross-version wire break. Freezing the JSON here makes such a change fail in
// CI instead of silently on a customer's host. See docs/coupling-decomposition-plan.md (B5).
func fullyPopulatedRequest() ExecutionRequest {
	return ExecutionRequest{
		ExecutionRunID: uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		UnifiedJobID:   4242,
		CreatedAt:      time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		JobManifest: JobManifest{
			InventoryID:         7,
			Inventory:           "[web]\nhost-1 ansible_host=10.0.0.1\n",
			ProjectURL:          "https://gitea.local/acme/playbooks.git",
			ProjectRef:          "main",
			Playbook:            "site.yml",
			PlaybookContent:     "- hosts: all\n  tasks: []\n",
			ExtraVars:           map[string]interface{}{"env": "prod", "retries": float64(3)},
			Limit:               "web",
			UseFactCache:        true,
			CachedFacts:         map[string]json.RawMessage{"host-1": json.RawMessage(`{"ansible_os_family":"Debian"}`)},
			InventorySync:       true,
			InventorySource:     "plugin: amazon.aws.aws_ec2\n",
			InventorySourceKind: "inventory",
			SyncInventoryID:     9,
			RunnerHost:          "bastion.acme.internal",
			RunnerHostID:        11,
			APIURL:              "https://praetor.local/api/v1",
			ExecutionPack:       "docker-tools",
			SSHUser:             "deploy",
			SSHPrivateKey:       "-----BEGIN OPENSSH PRIVATE KEY-----\nREDACTED\n-----END OPENSSH PRIVATE KEY-----\n",
			CredentialID:        13,
			CredentialEnv:       map[string]string{"AWS_ACCESS_KEY_ID": "AKIAEXAMPLE"},
			CredentialFiles:     map[string]string{"ANSIBLE_PRIVATE_KEY_FILE": "keydata"},
			GalaxyServers: []GalaxyServer{{
				Name:    "automation_hub",
				URL:     "https://hub.local/api/galaxy/",
				Token:   "galaxy-token",
				AuthURL: "https://hub.local/sso/token",
			}},
			IngestToken: "run-scoped-bearer-token",
		},
	}
}

// TestExecutionRequestGolden freezes the JSON wire shape of ExecutionRequest /
// JobManifest — the contract shared by the scheduler (writer), the executor
// (mutator) and the host-runner (reader, on its own release train).
func TestExecutionRequestGolden(t *testing.T) {
	got, err := json.MarshalIndent(fullyPopulatedRequest(), "", "  ")
	if err != nil {
		t.Fatalf("marshal ExecutionRequest: %v", err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "execution_request.golden.json")
	if wantUpdate() {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("ExecutionRequest JSON drifted from the frozen wire contract.\n"+
			"If this change is intentional, review host-runner compatibility "+
			"(additive-only fields; see docs/coupling-decomposition-plan.md B5) "+
			"then regenerate with:  go test ./pkg/events -run Golden -update\n\n--- got ---\n%s", got)
	}
}
