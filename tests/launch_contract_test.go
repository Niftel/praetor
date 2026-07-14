package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestLaunchIsSingleSited enforces the core invariant established by B2: a job or
// workflow run is created in exactly one place. Before pkg/launch, "start a job"
// was reimplemented at six independent INSERT INTO unified_jobs sites (and four
// workflow_jobs snapshots) across the api and scheduler, each hand-marshalling a
// different subset of job_args — which is how scheduled runs and workflow nodes
// silently dropped their overrides (bug #79).
//
// pkg/launch has since been extracted into its own module
// (github.com/praetordev/launch), which now owns the single canonical INSERT and
// its frozen column set. The monorepo-side guardrail is therefore the dual: there
// must be ZERO hand-rolled INSERT INTO unified_jobs / workflow_jobs anywhere in
// this repo — every launch must route through the launch module's launch.Job.
//
// If this fails because a hand-rolled INSERT appeared, don't update the test —
// route the new launch surface through github.com/praetordev/launch instead. The
// whole point is that there is one door.
//
// See docs/coupling-decomposition-plan.md (B1, B2).
func TestLaunchIsSingleSited(t *testing.T) {
	root := repoRoot(t)

	for _, table := range []string{"unified_jobs", "workflow_jobs"} {
		t.Run(table, func(t *testing.T) {
			sites := insertSites(t, root, table)
			if len(sites) != 0 {
				t.Fatalf("found %d hand-rolled `INSERT INTO %s` site(s) in the monorepo:\n  %s\n"+
					"Every launch must go through the launch module (github.com/praetordev/launch) — "+
					"route the new surface through launch.Job instead of adding an INSERT.",
					len(sites), table, strings.Join(sites, "\n  "))
			}
		})
	}
}

// insertSites scans every non-test .go file under root for `INSERT INTO <table> (`
// statements (ignoring line-comment mentions) and returns the matching files,
// repo-relative.
func insertSites(t *testing.T, root, table string) []string {
	t.Helper()
	re := regexp.MustCompile(`INSERT INTO ` + regexp.QuoteMeta(table) + `\s*\(`)
	var sites []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue // skip doc-comment mentions
			}
			if re.MatchString(line) {
				sites = append(sites, rel(root, path))
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan for INSERT INTO %s: %v", table, err)
	}
	return sites
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}

// repoRoot returns the repository root by walking up from this test file until a
// go.mod is found.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(self)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (go.mod)")
		}
		dir = parent
	}
}
