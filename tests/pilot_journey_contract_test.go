package tests

import (
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
