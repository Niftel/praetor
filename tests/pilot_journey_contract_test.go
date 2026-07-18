package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPilotJourneyCoversRealManagedHostBoundary(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-pilot-journey.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"pilot-managed-host", "approval_team_id", "requester self-approval returned HTTP", "frontend-team approval returned HTTP",
		"credential_id:$credential", "use_fact_cache:true", "Install pilot marker", `jobs/runs/$first_run/logs`,
		`changed=1([[:space:]]|$)`, `changed=0([[:space:]]|$)`, "hosts/$HOST_ID/facts", "resolution_count", "canceled\\|1",
		"notification-exact-once", "activity-stream?limit=500", "managed-host-journey.json", "shasum -a 256",
		"stage_execution_pack", "verify_execution_pack", "kubectl --context \"$CONTEXT\" -n \"$NAMESPACE\" cp",
		`SECRETS_DB_POD="${PRAETOR_STAGING_SECRETS_DB_POD:-${RELEASE}-secrets-postgres-0}"`, `get pod "$SECRETS_DB_POD"`,
		`docker exec "$PILOT_TARGET" rm -f /home/praetor/.praetor-pilot-marker`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("pilot journey contract is missing %q", required)
		}
	}
	for _, required := range []string{"Retry-After", `status" != 429`, "retrying in"} {
		if !strings.Contains(script, required) {
			t.Fatalf("pilot journey login is missing rate-limit handling %q", required)
		}
	}
	for _, forbidden := range []string{"cat $PRIVATE_KEY", "--rawfile key", "inputs.password", "Authorization: Bearer $TOKEN\" >"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("pilot journey contains unsafe secret handling %q", forbidden)
		}
	}
}

func TestPilotFaultMatrixCoversManagedHostFailureBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-pilot-journey.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"staging-pilot-journey-faults", "jobs/$canceled_unified/cancel", "post-cancel task",
		"duplicate active workflow launch", "rollout restart", "deployment/$RELEASE-scheduler",
		"docker network disconnect praetor-pilot", "120-second failure boundary", "actionable diagnostics",
		"binding_state", "notification_count", "activity-stream?limit=500", "managed-host-faults.json",
	} {
		if !strings.Contains(script+readMakefile(t, root), required) {
			t.Fatalf("pilot fault matrix is missing %q", required)
		}
	}
	playbook, err := os.ReadFile(filepath.Join(root, "playbooks", "pilot-managed-host-fault.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Record fault run start", "Hold the run open for fault injection", "Record fault run completion", "ansible.builtin.pause"} {
		if !strings.Contains(string(playbook), required) {
			t.Fatalf("pilot fault playbook is missing %q", required)
		}
	}
}

func TestPilotCredentialFaultMatrixUsesPersistentStagingBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-pilot-credential-faults.sh"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(raw) + readMakefile(t, root)
	for _, required := range []string{
		"staging-pilot-credential-faults", "test-secrets-execution-e2e.sh",
		"praetor-staging-secrets-postgres", "praetor-staging-audit-postgres",
		"praetor_secrets", "praetor_audit", "credential-faults.json",
		`current Kubernetes context is not`, `(.checks | length == 9)`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("pilot credential fault matrix is missing %q", required)
		}
	}
}

func TestPilotReadinessDecisionIsFailClosedAndSanitized(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "generate-pilot-readiness-report.sh"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(raw) + readMakefile(t, root)
	for _, required := range []string{
		"staging-pilot-readiness", "managed-host-pilot", "managed-host-pilot-faults",
		"credential-faults.json", "PRAETOR_EXECUTION_PACK_REVISION", "PRAETOR_TARGET_IMAGE_REVISION",
		"release-blocking", `select(.number != 173 and .number != 178)`, "chmod 0600",
		"sensitive material appeared in pilot readiness report",
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("pilot readiness decision is missing %q", required)
		}
	}
}

func TestRecordedPilotDecisionIsSanitizedAndComplete(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "deployments", "pilot", "managed-host-pilot-readiness.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Profile   string `json:"profile"`
		Revisions struct {
			Praetor        string            `json:"praetor"`
			SecretsService string            `json:"secrets_service"`
			Components     map[string]string `json:"components"`
			ExecutionPack  string            `json:"execution_pack"`
			TargetImage    string            `json:"target_image"`
		} `json:"revisions"`
		Journeys []struct {
			EvidenceSHA256 string `json:"evidence_sha256"`
		} `json:"journeys"`
		Findings []any `json:"findings"`
		Decision struct {
			Status  string   `json:"status"`
			Reasons []string `json:"reasons"`
		} `json:"decision"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Profile != "managed-host-pilot" || report.Decision.Status != "go" || len(report.Decision.Reasons) != 0 || len(report.Findings) != 0 {
		t.Fatalf("unexpected recorded pilot decision: %+v", report.Decision)
	}
	if len(report.Revisions.Praetor) != 40 || len(report.Revisions.SecretsService) != 40 || len(report.Revisions.Components) != 8 || len(report.Journeys) != 3 {
		t.Fatal("recorded pilot decision is missing exact revisions or journey digests")
	}
	for _, value := range append([]string{report.Revisions.ExecutionPack, report.Revisions.TargetImage}, report.Journeys[0].EvidenceSHA256, report.Journeys[1].EvidenceSHA256, report.Journeys[2].EvidenceSHA256) {
		if len(strings.TrimPrefix(value, "sha256:")) != 64 {
			t.Fatalf("invalid recorded digest %q", value)
		}
	}
	if strings.Contains(strings.ToLower(string(raw)), "password") || strings.Contains(string(raw), "172.29.") {
		t.Fatal("recorded pilot decision contains sensitive material")
	}
}

func readMakefile(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPilotPlaybookIsIdempotentAndCollectsFacts(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "playbooks", "pilot-managed-host.yml"))
	if err != nil {
		t.Fatal(err)
	}
	playbook := string(raw)
	for _, required := range []string{"hosts: all", "gather_facts: true", "ansible.builtin.copy", "/home/praetor/.praetor-pilot-marker", "ansible.builtin.assert"} {
		if !strings.Contains(playbook, required) {
			t.Fatalf("pilot playbook is missing %q", required)
		}
	}
	for _, forbidden := range []string{"shell:", "command:", "latest", "http://", "https://"} {
		if strings.Contains(playbook, forbidden) {
			t.Fatalf("pilot playbook contains non-deterministic construct %q", forbidden)
		}
	}
}

func TestPilotInventoryHostNameIsAnsibleSafe(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-pilot-access.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{"HOST_NAME=\"pilot-managed-host\"", "LEGACY_HOST_NAME=\"Pilot Managed Host\"", "put \"hosts/$HOST_ID\"", "PRAETOR_PILOT_ROTATE_CREDENTIAL", `-X PUT`} {
		if !strings.Contains(script, required) {
			t.Fatalf("pilot host migration contract is missing %q", required)
		}
	}
}
