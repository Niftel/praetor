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
		"bootstrap|validate-issue|sync-issue|sync-pr|verify-main|repair-project",
		`select(.name == "Status")`,
		"canonical_project_status",
		"repair_project",
		"set_project_status",
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
		"PROJECT_GH_TOKEN: ${{ secrets.PROJECT_AUTOMATION_TOKEN }}",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("development workflow must contain %q", required)
		}
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

func workflowContains(t *testing.T, root, value string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "development-flow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(raw), value)
}
