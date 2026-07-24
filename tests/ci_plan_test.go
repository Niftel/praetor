package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIPlannerSelectsSmallestSafeGateSet(t *testing.T) {
	tests := []struct {
		name  string
		paths string
		want  map[string]string
	}{
		{
			name:  "documentation does not allocate build runners",
			paths: "docs-site/docs/concepts/notifications.md\nREADME.md\n",
			want:  map[string]string{"run_go": "false", "run_ui": "false", "run_security": "false", "run_images": "false", "run_codeql": "false"},
		},
		{
			name:  "API handler gets Go database security and API image gates",
			paths: "services/api/handlers/notifications.go\n",
			want:  map[string]string{"run_go": "true", "run_database": "true", "run_security": "true", "images": "api", "run_codeql": "true"},
		},
		{
			name:  "UI test does not rebuild the production image",
			paths: "web/pages/NotificationSettings.test.tsx\n",
			want:  map[string]string{"run_ui": "true", "run_go": "false", "run_images": "false"},
		},
		{
			name:  "UI source builds only the UI image",
			paths: "web/pages/NotificationSettings.tsx\n",
			want:  map[string]string{"run_ui": "true", "images": "ui", "run_images": "true"},
		},
		{
			name:  "migration builds only the migrator image",
			paths: "db/migrations/0021_notification.sql\n",
			want:  map[string]string{"run_database": "true", "run_deployment": "false", "images": "migrator"},
		},
		{
			name:  "module changes rebuild both Go images",
			paths: "go.mod\ngo.sum\n",
			want:  map[string]string{"run_go": "true", "run_database": "true", "run_security": "true", "images": "api,migrator"},
		},
		{
			name:  "Helm changes run deployment and targeted product validation",
			paths: "deployments/helm/praetor-v2/templates/api.yaml\n",
			want:  map[string]string{"run_deployment": "true", "run_product": "true", "run_go": "false"},
		},
		{
			name:  "workflow changes always run workflow lint",
			paths: ".github/workflows/development-flow.yml\n",
			want:  map[string]string{"run_deployment": "true", "run_go": "false", "run_images": "false"},
		},
		{
			name:  "planner changes exercise every validation family",
			paths: "scripts/plan-ci.sh\n",
			want:  map[string]string{"run_go": "true", "run_ui": "true", "run_database": "true", "run_deployment": "true", "run_security": "true", "run_product": "true", "images": "api,migrator,ui"},
		},
	}

	root := repositoryRoot(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("bash", "./scripts/plan-ci.sh")
			cmd.Dir = root
			cmd.Stdin = strings.NewReader(tt.paths)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("plan CI: %v\n%s", err, output)
			}
			got := parseCIPlan(output)
			for key, value := range tt.want {
				if got[key] != value {
					t.Errorf("%s = %q, want %q; plan:\n%s", key, got[key], value, output)
				}
			}
		})
	}
}

func TestCIPlannerAllProducesValidCompleteImageMatrix(t *testing.T) {
	root := repositoryRoot(t)
	cmd := exec.Command("bash", "./scripts/plan-ci.sh", "--all")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plan all CI: %v\n%s", err, output)
	}
	plan := parseCIPlan(output)
	if plan["images"] != "api,migrator,ui" {
		t.Fatalf("images = %q, want api,migrator,ui", plan["images"])
	}
	for _, fragment := range []string{`"name":"api"`, `"name":"migrator"`, `"name":"ui"`} {
		if !bytes.Contains([]byte(plan["image_matrix"]), []byte(fragment)) {
			t.Errorf("image_matrix missing %s: %s", fragment, plan["image_matrix"])
		}
	}
	for _, fragment := range []string{`"language":"go"`, `"language":"javascript-typescript"`} {
		if !bytes.Contains([]byte(plan["codeql_matrix"]), []byte(fragment)) {
			t.Errorf("codeql_matrix missing %s: %s", fragment, plan["codeql_matrix"])
		}
	}
}

func parseCIPlan(output []byte) map[string]string {
	plan := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			plan[key] = value
		}
	}
	return plan
}

func TestChangeAwareWorkflowsKeepStableGatesAndAvoidMainDuplication(t *testing.T) {
	root := repositoryRoot(t)
	readWorkflow := func(name string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	ci := readWorkflow("test.yml")
	for _, required := range []string{
		"./scripts/github-ci-plan.sh", "needs: classify", "test:",
		"needs: [classify, go, deployment-contracts, ui, database-compatibility]",
		"success|skipped",
	} {
		if !strings.Contains(ci, required) {
			t.Errorf("CI workflow must contain %q", required)
		}
	}
	if strings.Contains(ci, "push:\n    branches: [main]") {
		t.Error("PR CI must not repeat on main")
	}

	image := readWorkflow("image.yml")
	for _, required := range []string{
		"artifact-metadata: write",
		"image_matrix: ${{ steps.plan.outputs.image_matrix }}",
		"matrix: ${{ fromJSON(needs.classify.outputs.image_matrix) }}",
		"if: needs.classify.outputs.run_images == 'true'",
		"name: image-gate",
		"push-to-registry: true",
		"create-storage-record: true",
	} {
		if !strings.Contains(image, required) {
			t.Errorf("Image workflow must contain %q", required)
		}
	}
	if strings.Contains(image, "- name: api\n            context:") {
		t.Error("Image workflow must not retain a fixed all-image matrix")
	}

	for _, name := range []string{"codeql.yml", "gosec.yml", "govulncheck.yml"} {
		workflow := readWorkflow(name)
		if !strings.Contains(workflow, "./scripts/github-ci-plan.sh") {
			t.Errorf("%s must use the shared CI planner", name)
		}
		if strings.Contains(workflow, "push:\n    branches: [main]") {
			t.Errorf("%s must not repeat its PR gate on main", name)
		}
	}
	if strings.Contains(readWorkflow("gosec.yml"), "schedule:") {
		t.Error("pinned gosec must not consume a scheduled runner without source changes")
	}

	developmentFlow := readWorkflow("development-flow.yml")
	if !strings.Contains(developmentFlow, `workflows: ["Image"]`) {
		t.Error("development flow must reconcile main once from the authoritative Image workflow")
	}
	for _, duplicate := range []string{`"CI",`, `"CodeQL",`, `"Go vulnerability scan",`, `"Go security scan",`} {
		if strings.Contains(developmentFlow, duplicate) {
			t.Errorf("development flow retains duplicate workflow_run trigger %s", duplicate)
		}
	}
}
