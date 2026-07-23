package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentFlowIssueValidation(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	event := filepath.Join(temp, "event.json")
	body := `{"issue":{"body":"## Outcome\nVisible result\n## Scope\nIncluded and excluded\n## Acceptance criteria\n- [ ] observable result\n## Required tests\nIntegration test\n## Security and RBAC impact\nNone\n## Dependencies\nNone"}}`
	if err := os.WriteFile(event, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(temp, "bin")
	if err := os.Mkdir(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gh", "jq"} {
		target, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s is not installed", name)
		}
		if err := os.Symlink(target, filepath.Join(fakeBin, name)); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", "scripts/development-flow.sh", "validate-issue")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+":"+os.Getenv("PATH"), "EVENT_PATH="+event)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("valid issue rejected: %v\n%s", err, output)
	}
}

func TestDevelopmentFlowReportsUnlinkedHumanPullRequest(t *testing.T) {
	root := repositoryRoot(t)
	event := filepath.Join(t.TempDir(), "event.json")
	body := `{"action":"opened","pull_request":{"body":"No closing reference","merged":false}}`
	if err := os.WriteFile(event, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "scripts/development-flow.sh", "sync-pr")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "EVENT_PATH="+event)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("unlinked human pull request was accepted")
	}
	if !strings.Contains(string(output), "PR must use Closes/Fixes/Resolves #issue") {
		t.Fatalf("missing explicit link error; output:\n%s", output)
	}
}

func TestDevelopmentFlowIsRepositoryDriven(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "development-flow.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"bootstrap|validate-issue|sync-issue|sync-pr|verify-main|repair-project|audit-completion|close-milestone",
		`select(.name == "Status")`,
		"canonical_project_status",
		"repair_project",
		"set_project_status",
		`.repository == $repository`,
		"PROJECT_AUTOMATION_TOKEN",
		"PROJECT_GH_TOKEN",
		"GH_TOKEN: ${{ github.token }}",
	} {
		if !strings.Contains(script, required) &&
			!workflowContains(t, root, required) {
			t.Fatalf("development pipeline must contain %q", required)
		}
	}
	if strings.Contains(script, `--name "Workflow Status"`) ||
		strings.Contains(script, ".workflow_status") {
		t.Fatal("development pipeline must not create or update a duplicate workflow status field")
	}
}

func TestDevelopmentFlowStateUsesCanonicalProjectStatus(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "development-flow-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	state := string(raw)
	for _, required := range []string{`"project_status"`, `"Todo"`, `"In Progress"`, `"Done"`} {
		if !strings.Contains(state, required) {
			t.Fatalf("canonical project state must contain %s", required)
		}
	}
	if strings.Contains(state, `"workflow_status"`) || strings.Contains(state, `"Verification"`) {
		t.Fatal("generated state must not retain the duplicate six-stage status field")
	}
}

func TestDevelopmentFlowSeparatesRepositoryAndProjectTokens(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "development-flow.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if !strings.Contains(script, `GH_TOKEN="$PROJECT_GH_TOKEN" gh "$@"`) {
		t.Fatal("project commands must explicitly use PROJECT_GH_TOKEN")
	}
	if strings.Contains(script, `GH_TOKEN="$PROJECT_GH_TOKEN" gh label`) ||
		strings.Contains(script, `GH_TOKEN="$PROJECT_GH_TOKEN" gh issue`) {
		t.Fatal("repository labels and issues must not use the organization project token")
	}
}

func TestDevelopmentFlowExposesCanonicalStatusRepair(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "development-flow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	for _, required := range []string{
		"repair_project:",
		"./scripts/development-flow.sh repair-project",
		"audit_completion:",
		"./scripts/development-flow.sh audit-completion",
		`cron: "29 5 * * *"`,
		"PROJECT_GH_TOKEN: ${{ secrets.PROJECT_AUTOMATION_TOKEN }}",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("development workflow must contain %q", required)
		}
	}
}

func TestDevelopmentFlowRejectsClosedMilestoneWithUnverifiedIssue(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	writeFakeGH(t, temp, `#!/usr/bin/env bash
if [[ "$1 $2" == "issue list" ]]; then
  printf '%s\n' '[{"number":157,"state":"CLOSED","labels":[{"name":"flow:verification"}],"url":"https://github.com/Niftel/praetor/issues/157"}]'
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
`)
	cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; audit_milestone_issues Next`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+temp+":"+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("closed milestone with a Verification issue passed completion audit")
	}
	if !strings.Contains(string(output), "#157 state=CLOSED flow=flow:verification") {
		t.Fatalf("audit did not identify the inconsistent issue; output:\n%s", output)
	}
}

func TestDevelopmentFlowRefusesUnsafeMilestoneClosure(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	marker := filepath.Join(temp, "patched")
	writeFakeGH(t, temp, `#!/usr/bin/env bash
if [[ "$1" == "api" && "$*" == *"milestones?state=all"* ]]; then
  printf '%s\n' '[{"number":2,"title":"Next","state":"open"}]'
elif [[ "$1 $2" == "issue list" ]]; then
  printf '%s\n' '[{"number":157,"state":"CLOSED","labels":[{"name":"flow:verification"}],"url":"https://github.com/Niftel/praetor/issues/157"}]'
elif [[ "$1" == "api" && "$2" == "--method" ]]; then
  touch "$FAKE_PATCH_MARKER"
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
`)
	cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; close_milestone Next`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+temp+":"+os.Getenv("PATH"), "FAKE_PATCH_MARKER="+marker)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("unsafe milestone closure succeeded:\n%s", output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("milestone PATCH was attempted after completion audit failed")
	}
}

func TestDevelopmentFlowRejectsProjectStatusMismatch(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	writeFakeGH(t, temp, `#!/usr/bin/env bash
if [[ "$1 $2" == "project item-list" ]]; then
  printf '%s\n' '{"items":[{"content":{"type":"Issue","number":157},"repository":"https://github.com/Niftel/praetor","labels":["flow:verification"],"status":"Done"}]}'
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
`)
	cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; audit_project_statuses`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+temp+":"+os.Getenv("PATH"), "PROJECT_GH_TOKEN=test-token")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("project status mismatch passed completion audit")
	}
	if !strings.Contains(string(output), "#157 project=Done flow=flow:verification expected=In Progress") {
		t.Fatalf("audit did not describe the project mismatch; output:\n%s", output)
	}
}

func TestDevelopmentFlowRepairScopesAndSelectsOnlyMismatchedPraetorIssues(t *testing.T) {
	root := repositoryRoot(t)
	project := `{"items":[
		{"id":"other-repo","content":{"type":"Issue","number":21},"repository":"https://github.com/Niftel/eventbus","labels":["flow:done"],"status":"In Progress"},
		{"id":"pull-request","content":{"type":"PullRequest","number":21},"repository":"https://github.com/Niftel/praetor","labels":["flow:done"],"status":"In Progress"},
		{"id":"already-correct","content":{"type":"Issue","number":301},"repository":"https://github.com/Niftel/praetor","labels":["flow:done"],"status":"Done"},
		{"id":"needs-repair","content":{"type":"Issue","number":302},"repository":"https://github.com/Niftel/praetor","labels":["flow:done"],"status":"In Progress"}
	]}`
	cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; project_issue_repairs`)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(project)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("project issue filtering failed: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "needs-repair\tDone\t302"; got != want {
		t.Fatalf("project repairs = %q, want %q", got, want)
	}
}

func TestDevelopmentFlowRepairRejectsAmbiguousFlowLabels(t *testing.T) {
	root := repositoryRoot(t)
	project := `{"items":[{"id":"ambiguous","content":{"type":"Issue","number":302},"repository":"https://github.com/Niftel/praetor","labels":["flow:done","flow:verification"],"status":"In Progress"}]}`
	cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; project_issue_repairs`)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(project)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("ambiguous flow labels were silently repaired:\n%s", output)
	}
	if !strings.Contains(string(output), "must have exactly one flow label") {
		t.Fatalf("repair did not explain ambiguous labels:\n%s", output)
	}
}

func TestDevelopmentFlowRepairValidatesSnapshotBeforeEditing(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	marker := filepath.Join(temp, "edited")
	writeFakeGH(t, temp, `#!/usr/bin/env bash
if [[ "$1 $2" == "project item-list" ]]; then
  printf '%s\n' '{"items":[{"id":"valid","content":{"type":"Issue","number":301},"repository":"https://github.com/Niftel/praetor","labels":["flow:done"],"status":"In Progress"},{"id":"ambiguous","content":{"type":"Issue","number":302},"repository":"https://github.com/Niftel/praetor","labels":["flow:done","flow:verification"],"status":"In Progress"}]}'
elif [[ "$1 $2" == "project item-edit" ]]; then
  touch "$FAKE_EDIT_MARKER"
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
`)
	cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; repair_project`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+temp+":"+os.Getenv("PATH"), "PROJECT_GH_TOKEN=test-token", "FAKE_EDIT_MARKER="+marker)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("repair accepted an invalid snapshot:\n%s", output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("project item was edited before the full snapshot passed validation")
	}
}

func TestDevelopmentFlowRejectsStaleClosedVerification(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	writeFakeGH(t, temp, `#!/usr/bin/env bash
if [[ "$1 $2" == "issue list" ]]; then
  printf '%s\n' '[{"number":267,"closedAt":"2026-07-20T00:00:00Z","labels":[{"name":"flow:verification"}],"url":"https://github.com/Niftel/praetor/issues/267"}]'
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
`)
	cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; audit_stale_closed_issues`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+temp+":"+os.Getenv("PATH"), "AUDIT_NOW_EPOCH=1784764800")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("stale closed Verification issue passed completion audit")
	}
	if !strings.Contains(string(output), "#267 closedAt=2026-07-20T00:00:00Z flow=flow:verification") {
		t.Fatalf("audit did not identify stale Verification; output:\n%s", output)
	}
}

func TestDevelopmentFlowVerifiesRequiredPullRequestWorkflows(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	config := filepath.Join(temp, "flow.json")
	if err := os.WriteFile(config, []byte(`{
		"owner":"Niftel",
		"repository":"praetor",
		"required_workflows":["CI","Image"]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFakeGH(t, temp, `#!/usr/bin/env bash
if [[ "$1 $2" == "pr checks" ]]; then
  printf '%s\n' "$FAKE_CHECKS"
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
`)

	tests := []struct {
		name    string
		checks  string
		wantErr bool
	}{
		{
			name:   "all required workflows succeeded",
			checks: `[{"workflow":"CI","state":"SUCCESS"},{"workflow":"CI","state":"SKIPPED"},{"workflow":"Image","state":"SUCCESS"}]`,
		},
		{
			name:    "required workflow missing",
			checks:  `[{"workflow":"CI","state":"SUCCESS"}]`,
			wantErr: true,
		},
		{
			name:    "required workflow contains failure",
			checks:  `[{"workflow":"CI","state":"SUCCESS"},{"workflow":"Image","state":"SUCCESS"},{"workflow":"Image","state":"FAILURE"}]`,
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; required_pr_workflows_succeeded 340`)
			cmd.Dir = root
			cmd.Env = append(
				os.Environ(),
				"PATH="+temp+":"+os.Getenv("PATH"),
				"PRAETOR_DEVELOPMENT_FLOW_CONFIG="+config,
				"FAKE_CHECKS="+test.checks,
			)
			output, err := cmd.CombinedOutput()
			if test.wantErr && err == nil {
				t.Fatalf("invalid workflow state passed verification:\n%s", output)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("valid workflow state failed verification: %v\n%s", err, output)
			}
		})
	}
}

func writeFakeGH(t *testing.T, directory, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "gh"), []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentFlowSkipsDependabotPullRequestsByAuthor(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "development-flow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	if !strings.Contains(workflow, "github.event.pull_request.user.login != 'dependabot[bot]'") {
		t.Fatal("Dependabot exclusion must use pull request author, not event actor")
	}
	if strings.Contains(workflow, "github.actor != 'dependabot[bot]'") {
		t.Fatal("event actor changes when a maintainer updates a Dependabot branch")
	}
}

func TestDevelopmentFlowSerializesAndReconcilesOutOfOrderEvents(t *testing.T) {
	root := repositoryRoot(t)
	workflowRaw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "development-flow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(workflowRaw)
	for _, required := range []string{
		"group: development-flow-state-${{ github.repository }}",
		"cancel-in-progress: false",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("development events must be serialized with %q", required)
		}
	}

	scriptRaw, err := os.ReadFile(filepath.Join(root, "scripts", "development-flow.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, required := range []string{
		"authoritative_issue_status",
		"has_open_linked_pr",
		`reconcile_issue "$number" "$url" "Backlog"`,
		`reconcile_issue "$issue" "$issue_url" "In Review"`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("out-of-order reconciliation must contain %q", required)
		}
	}
}

func TestDevelopmentFlowOpenPullRequestWinsOverDelayedBacklogEvent(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	fakeGH := filepath.Join(temp, "gh")
	fake := `#!/usr/bin/env bash
if [[ "$1 $2" == "issue view" ]]; then
  printf '%s\n' '{"state":"OPEN","labels":[{"name":"flow:backlog"}]}'
elif [[ "$1 $2" == "pr list" ]]; then
  printf '%s\n' "${FAKE_PULLS:-[] }"
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
`
	if err := os.WriteFile(fakeGH, []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}

	run := func(pulls string) string {
		t.Helper()
		cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; authoritative_issue_status 201 Backlog`)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "PATH="+temp+":"+os.Getenv("PATH"), "FAKE_PULLS="+pulls)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("status resolution failed: %v\n%s", err, output)
		}
		return strings.TrimSpace(string(output))
	}

	if got := run(`[]`); got != "Backlog" {
		t.Fatalf("unlinked issue status = %q, want Backlog", got)
	}
	if got := run(`[{"body":"Closes #201"}]`); got != "In Review" {
		t.Fatalf("delayed issue-open status = %q, want In Review", got)
	}
}

func TestDevelopmentFlowPreservesCurrentStateOnIssueEdit(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	writeFakeGH(t, temp, `#!/usr/bin/env bash
if [[ "$1 $2" == "issue view" ]]; then
  printf '%s\n' '{"state":"OPEN","labels":[{"name":"flow:in-progress"}]}'
elif [[ "$1 $2" == "pr list" ]]; then
  printf '%s\n' '[]'
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
`)
	cmd := exec.Command("bash", "-c", `source scripts/development-flow.sh; authoritative_issue_status 302 Backlog`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+temp+":"+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status resolution failed: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "In Progress" {
		t.Fatalf("edited issue status = %q, want In Progress", got)
	}

	scriptRaw, err := os.ReadFile(filepath.Join(root, "scripts", "development-flow.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scriptRaw), `[[ "$action" == opened || "$action" == edited ]]`) {
		t.Fatal("corrected edited issues must be synchronized after validation")
	}
}

func workflowContains(t *testing.T, root, value string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "development-flow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(raw), value)
}
