package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobsSurfaceUsesUnifiedTypeAwareExecutionList(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "web", "pages", "JobsPage.tsx")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, contract := range []string{
		"api.getJobs()",
		"api.getWorkflowJobs()",
		"Playbook job",
		"Workflow job",
		"Filter jobs by type",
		"Search jobs by name or ID",
		"View output",
	} {
		if !strings.Contains(source, contract) {
			t.Errorf("JobsPage must retain AAP-style execution-list contract %q", contract)
		}
	}
	for _, forbidden := range []string{
		"Runs · last 48h",
		"Select template…",
		"convergedPct",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("JobsPage must not retain dashboard/launcher clutter %q", forbidden)
		}
	}
}
