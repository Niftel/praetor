package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/praetordev/store"
)

func TestRunDiagnosticsWireShapeExcludesRawData(t *testing.T) {
	outcome := "failed"
	failure := "task_failed"
	result := FromRunDiagnostics(store.DiagnosticSummary{
		UnifiedJobID: 9, RunState: "failed", CurrentPhase: "complete", Attempt: 2,
		SafeFailureCode: failure,
	}, []store.DiagnosticEvent{{
		Seq: 7, EventType: "HOST_FAILED", Outcome: &outcome,
		FailureCode: &failure, CreatedAt: time.Unix(1, 0),
	}}, nil)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"event_data", "stdout", "stderr", "module_args", "launch_inputs", "secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics response exposed forbidden field %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"failure_code":"task_failed"`) {
		t.Fatalf("safe failure category missing: %s", text)
	}
}
