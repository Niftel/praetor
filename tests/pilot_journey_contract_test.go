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
