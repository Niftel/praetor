package tests

import (
	"encoding/json"
	"os"
	"os/exec"
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
		Version  int `json:"version"`
		Findings []struct {
			TrackingIssue int `json:"tracking_issue"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(baselineRaw, &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.Version != 1 || len(baseline.Findings) > 13 {
		t.Fatalf("gosec baseline schema/count = %d/%d; schema must remain 1 and the initial 13-finding ceiling may only shrink", baseline.Version, len(baseline.Findings))
	}
	for index, finding := range baseline.Findings {
		if finding.TrackingIssue <= 0 {
			t.Fatalf("gosec baseline finding %d has no positive tracking_issue", index)
		}
	}
}

func TestGosecBaselineRejectsUntrackedAcceptedFinding(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed")
	}
	root := repoRoot(t)
	temp := t.TempDir()
	baseline := filepath.Join(temp, "baseline.json")
	report := filepath.Join(temp, "report.sarif")
	if err := os.WriteFile(baseline, []byte(`{"version":1,"findings":[{"rule":"G101","file":"x.go","message":"m","snippet":"s"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, []byte(`{"runs":[{"tool":{"driver":{"rules":[]}},"results":[]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "scripts/check-gosec-baseline.sh", report)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOSEC_BASELINE="+baseline)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("untracked accepted gosec finding passed baseline validation")
	}
	if !strings.Contains(string(output), "must reference a positive tracking_issue") {
		t.Fatalf("missing tracking issue error; output:\n%s", output)
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
