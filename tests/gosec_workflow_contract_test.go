package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGosecWorkflowIsPinnedVisibleAndBlockingForRegressions(t *testing.T) {
	root := repoRoot(t)
	workflowRaw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "gosec.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(workflowRaw)
	for _, required := range []string{
		"name: Go security scan",
		"security-events: write",
		"make gosec GOSEC_REPORT=gosec.sarif",
		"continue-on-error: true",
		"github/codeql-action/upload-sarif@1ad29ea4a422cce9a242a9fae469541dcd08addc",
		"SCAN_OUTCOME: ${{ steps.scan.outcome }}",
		`test "$SCAN_OUTCOME" = success`,
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("gosec workflow is missing %q", required)
		}
	}

	makefileRaw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"GOSEC_VERSION ?= v2.28.0", "github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)", "check-gosec-baseline.sh"} {
		if !strings.Contains(string(makefileRaw), required) {
			t.Errorf("local gosec target is missing %q", required)
		}
	}

	flowRaw, err := os.ReadFile(filepath.Join(root, ".github", "development-flow.json"))
	if err != nil {
		t.Fatal(err)
	}
	var flow struct {
		RequiredWorkflows []string `json:"required_workflows"`
	}
	if err := json.Unmarshal(flowRaw, &flow); err != nil {
		t.Fatal(err)
	}
	if !containsString(flow.RequiredWorkflows, "Go security scan") {
		t.Fatal("development flow must wait for the Go security scan")
	}

	baselineRaw, err := os.ReadFile(filepath.Join(root, ".github", "gosec-high-baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	var baseline struct {
		Version  int               `json:"version"`
		Findings []json.RawMessage `json:"findings"`
	}
	if err := json.Unmarshal(baselineRaw, &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.Version != 1 || len(baseline.Findings) > 13 {
		t.Fatalf("gosec baseline schema/count = %d/%d; schema must remain 1 and the initial 13-finding ceiling may only shrink", baseline.Version, len(baseline.Findings))
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
