package dto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestWireGolden pins the JSON wire shape of every DTO. Each DTO is populated with
// deterministic values and marshaled; the bytes must match the committed golden in
// testdata/. This is the API's wire contract now that it is decoupled from
// github.com/praetordev/models — a renamed json tag, a changed type or a
// reordered field shows up as a golden diff instead of silently changing what the
// frontend receives.
//
// Regenerate the goldens after an intentional contract change with:
//
//	UPDATE_GOLDEN=1 go test ./services/api/dto/
//
// Add a case here for every DTO added to this package.
func TestWireGolden(t *testing.T) {
	desc, branch := "a description", "main"
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	runID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	i64 := func(v int64) *int64 { return &v }
	raw := json.RawMessage(`{"k":"v"}`)

	cases := []struct {
		name  string
		value any
	}{
		{"project", Project{ID: 7, OrganizationID: 3, Name: "infra", Description: &desc, SCMType: "git", SCMURL: "u", SCMBranch: &branch, CreatedAt: now, ModifiedAt: now}},
		{"organization", Organization{ID: 1, Name: "acme", Description: &desc, CreatedAt: now, ModifiedAt: now}},
		{"user", User{ID: 2, Username: "alice", FirstName: &desc, Email: &desc, IsSuperuser: true, IsActive: true, LdapDN: &desc, LdapSyncedAt: &now, CreatedAt: now, ModifiedAt: now}},
		{"team", Team{ID: 3, OrganizationID: 1, Name: "ops", Description: &desc, CreatedAt: now, ModifiedAt: now}},
		{"inventory", Inventory{ID: 4, OrganizationID: 1, Name: "inv", Description: &desc, Kind: "static", Content: &desc, CreatedAt: now, ModifiedAt: now}},
		{"host", Host{ID: 5, InventoryID: 4, Name: "h1", Description: &desc, Variables: raw, Enabled: true, IsControlNode: true, IsRunnerHost: true, RunnerLastSeen: &now, RunnerHealthy: true, CreatedAt: now, ModifiedAt: now}},
		{"group", Group{ID: 6, InventoryID: 4, Name: "g1", Description: &desc, Variables: raw, CreatedAt: now, ModifiedAt: now}},
		{"credential_type", CredentialType{ID: 7, Name: "ssh", Description: &desc, Inputs: raw, Injectors: raw, Managed: true, CreatedAt: now, ModifiedAt: now}},
		{"credential", Credential{ID: 8, OrganizationID: 1, CredentialTypeID: 7, Name: "c1", Description: &desc, Inputs: raw, CreatedAt: now, ModifiedAt: now}},
		{"job_template", JobTemplate{ID: 9, OrganizationID: 1, Name: "jt", Description: &desc, InventoryID: i64(4), ProjectID: i64(7), Playbook: "p.yml", PlaybookContent: &desc, UnifiedJobTemplateID: i64(9), CredentialID: i64(8), ExecutionPackID: i64(1), Forks: 5, JobType: "run", Verbosity: 1, ExtraVars: raw, JobLimit: "all", AskVariablesOnLaunch: true, AskLimitOnLaunch: true, SurveyEnabled: true, SurveySpec: raw, WebhookEnabled: true, WebhookService: "github", WebhookKey: "k", UseFactCache: true, AllowSimultaneous: true, CreatedAt: now, ModifiedAt: now}},
		{"schedule", Schedule{ID: 10, Name: "sch", Description: &desc, UnifiedJobTemplateID: i64(9), WorkflowTemplateID: i64(2), RRule: "FREQ=DAILY", NextRun: now, Enabled: true, ExtraVars: raw, CreatedAt: now, ModifiedAt: now}},
		{"unified_job", UnifiedJob{ID: 11, UnifiedJobTemplateID: i64(9), Name: "job", Status: "successful", CurrentRunID: &runID, CreatedAt: now, StartedAt: &now, FinishedAt: &now, CancelRequested: true, JobArgs: raw}},
		{"execution_run", ExecutionRun{ID: runID, UnifiedJobID: 11, CreatedAt: now, StartedAt: &now, FinishedAt: &now, State: "running", LastHeartbeatAt: &now, LastEventSeq: 42, PersistedEventSeq: 40}},
		{"job_event", JobEvent{ID: 12, UnifiedJobID: 11, ExecutionRunID: runID, Seq: 3, EventType: "runner_on_ok", HostID: i64(5), TaskName: &desc, PlayName: &desc, EventData: raw, StdoutSnippet: &desc, CreatedAt: now}},
	}

	update := os.Getenv("UPDATE_GOLDEN") != ""
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := json.MarshalIndent(c.value, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got = append(got, '\n')
			path := filepath.Join("testdata", c.name+".json")
			if update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("wire JSON drifted from golden %s\n got: %s\nwant: %s", path, got, want)
			}
		})
	}
}
