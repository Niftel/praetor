package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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
// If this test fails because a new hand-rolled INSERT appeared, don't update the
// test — route the new launch surface through pkg/launch instead. The whole
// point is that there is one door.
//
// See docs/coupling-decomposition-plan.md (B1, B2).
func TestLaunchIsSingleSited(t *testing.T) {
	root := repoRoot(t)

	for _, tc := range []struct {
		table       string
		wantFile    string
		wantColumns []string // the frozen column set of the single insert
	}{
		{
			table:       "unified_jobs",
			wantFile:    "pkg/launch/launch.go",
			wantColumns: []string{"name", "unified_job_template_id", "status", "created_at", "job_args"},
		},
		{
			table:    "workflow_jobs",
			wantFile: "pkg/launch/launch.go",
			// The workflow-run row; nodes/edges are snapshotted in the same function.
			// launch_args carries workflow-level overrides (schedule extra_vars,
			// webhook payload, EDA event+limit) overlaid on each node job (#90).
			wantColumns: []string{"workflow_template_id", "status", "launch_args"},
		},
	} {
		t.Run(tc.table, func(t *testing.T) {
			sites := insertSites(t, root, tc.table)
			if len(sites) != 1 {
				t.Fatalf("expected exactly one `INSERT INTO %s` site in non-test code, found %d:\n  %s\n"+
					"Every launch must go through pkg/launch — route the new surface there instead of adding an INSERT.",
					tc.table, len(sites), strings.Join(siteFiles(sites), "\n  "))
			}
			if got := rel(root, sites[0].file); got != tc.wantFile {
				t.Errorf("the single `INSERT INTO %s` moved to %s (want %s)", tc.table, got, tc.wantFile)
			}
			if !equalStringSets(sites[0].columns, tc.wantColumns) {
				t.Errorf("`INSERT INTO %s` columns changed:\n  got:  %v\n  want: %v", tc.table, sites[0].columns, tc.wantColumns)
			}
		})
	}
}

type insertSite struct {
	file    string
	columns []string
}

func siteFiles(s []insertSite) []string {
	var out []string
	for _, x := range s {
		out = append(out, x.file)
	}
	return out
}

// insertSites scans every non-test .go file under root for
// `INSERT INTO <table> (...)` statements and returns each match's column list.
// Matches inside line comments are ignored so doc comments don't count as sites.
func insertSites(t *testing.T, root, table string) []insertSite {
	t.Helper()
	re := regexp.MustCompile(`INSERT INTO ` + regexp.QuoteMeta(table) + `\s*\(([^)]*)\)`)
	var sites []insertSite
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
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				sites = append(sites, insertSite{file: path, columns: splitColumns(m[1])})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan for INSERT INTO %s: %v", table, err)
	}
	return sites
}

func splitColumns(s string) []string {
	var cols []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
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
