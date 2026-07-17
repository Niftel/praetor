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

func TestDevelopmentFlowIsRepositoryDriven(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "development-flow.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"bootstrap|validate-issue|sync-issue|sync-pr|verify-main",
		"Workflow Status",
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
