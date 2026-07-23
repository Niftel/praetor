package tests

import (
	"os"
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
		"notification-deliveries?organization_id=",
		"job_template",
		"inventory_source",
		"workflow_template",
		"workflow approval",
		"deployment/$RELEASE-consumer\" --replicas=0",
		"deployment/$RELEASE-scheduler",
		"deployment/$RELEASE-consumer",
		"deployment/praetor-validation-notification-sink --replicas=0",
		"transient_failure",
		"permanent_failure",
		".attempt_count == 2",
		".attempt_count == 1",
		"duplicate workflow occurrence",
		"wrong-team user",
		"cross-organization history returned",
		"notification-history-secret-redaction",
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
		`config:$`,
		"idempotency_key:$",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("notification delivery journey contains forbidden operation or evidence field %q", forbidden)
		}
	}
}
