package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationDeliveryJourneyCoversDurabilityRBACAndRedaction(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-notification-delivery-e2e.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"target-test-delivery",
		"notification-deliveries?organization_id=",
		"job_template",
		"inventory_source",
		`wait_history_state "$SUCCESS_NAME" "$INVENTORY_JOB_ID" success delivered`,
		"workflow_template",
		"workflow approval",
		`"deployment/$RELEASE-consumer" --replicas=0`,
		`"deployment/$RELEASE-scheduler"`,
		`"deployment/$RELEASE-consumer"`,
		`"deployment/$SINK_DEPLOYMENT" --replicas=0`,
		"transient_failure",
		"permanent_failure",
		".attempt_count == 2",
		".attempt_count == 1",
		"duplicate workflow occurrence",
		"wrong-team user",
		"unrelated-team operator can inspect organization-scoped job history",
		"cross-organization history returned",
		"notification-history-secret-redaction",
		"fixture-resource-cleanup",
		"PRAETOR_NOTIFICATION_EVIDENCE_FILE",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("notification delivery journey must contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"kubectl delete namespace",
		"k3d cluster delete",
		"helm uninstall",
		`wait_job "$INVENTORY_JOB_ID"`,
		`config:$`,
		"idempotency_key:$",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("notification delivery journey contains forbidden operation or evidence field %q", forbidden)
		}
	}
}

func TestPersistentStagingNotificationJourneyIsBoundedAndRepeatable(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-staging-notification-operations.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		`plan|status|run`,
		`PRAETOR_VALIDATION_CONTEXT="$CONTEXT"`,
		`PRAETOR_NOTIFICATION_SINK_DEPLOYMENT="$SINK"`,
		`PRAETOR_NOTIFICATION_EVIDENCE_FILE="$EVIDENCE_FILE"`,
		`staging-acceptance.sh" seed`,
		`reset_receiver`,
		`target-test-delivery`,
		`fixture-resource-cleanup`,
		`receiver-data-cleanup`,
		`notification-history-secret-canary`,
		`chmod 0600 "$EVIDENCE_FILE"`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("persistent staging notification journey must contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"k3d cluster delete",
		"kubectl delete namespace",
		"helm uninstall",
		"kubectl delete pvc",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("persistent staging notification journey contains forbidden cleanup %q", forbidden)
		}
	}

	cmd := exec.Command("bash", "scripts/validate-staging-notification-operations.sh", "plan")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("persistent staging notification plan failed: %v\n%s", err, output)
	}
	for _, expected := range []string{
		"Persistent staging notification readiness plan",
		"target test delivery",
		"bounded transient retry",
		"secret redaction",
		"receiver-log cleanup",
	} {
		if !bytes.Contains(output, []byte(expected)) {
			t.Errorf("persistent staging notification plan is missing %q:\n%s", expected, output)
		}
	}
}
