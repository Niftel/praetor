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

// TestLaunchSurfaceContract freezes the column set each launch surface writes to
// unified_jobs. Today, starting a job is reimplemented at six independent
// INSERT sites across two services (coupling hotspot H1); this test documents
// and pins that divergence so the pkg/launch extraction (B2) shows up as a
// deliberate, reviewable change to this contract rather than a silent drift.
//
// It reads the source directly (rather than exercising a DB) because the point
// is to freeze *what each site writes* — including the two surfaces that omit
// job_args, which is exactly why scheduled runs and workflow nodes can't carry
// overrides today (bug #79 and the workflow-node gap). When B2 routes all six
// through launch.Job/launch.Workflow, update the expectations below to the
// unified column set.
//
// See docs/coupling-decomposition-plan.md (B1, B2).
func TestLaunchSurfaceContract(t *testing.T) {
	root := repoRoot(t)

	type surface struct {
		file    string   // path relative to repo root
		desc    string   // what launches through here
		columns []string // exact unified_jobs columns this site INSERTs
		jobArgs bool     // whether it carries per-launch overrides (job_args)
	}

	// The frozen contract. Two surfaces deliberately omit job_args — that is the
	// override-dropping divergence B2 fixes, captured here so the fix is visible.
	surfaces := []surface{
		{
			file:    "services/api/store/job_store.go",
			desc:    "manual launch (POST /job-templates/{id}/launch)",
			columns: []string{"name", "unified_job_template_id", "status", "created_at", "job_args"},
			jobArgs: true,
		},
		{
			file:    "services/api/store/webhook_store.go",
			desc:    "inbound webhook launch",
			columns: []string{"name", "unified_job_template_id", "status", "created_at", "job_args"},
			jobArgs: true,
		},
		{
			file:    "services/api/store/event_store.go",
			desc:    "EDA event-rule launch",
			columns: []string{"name", "unified_job_template_id", "status", "created_at", "job_args"},
			jobArgs: true,
		},
		{
			file:    "services/api/store/inventory_store.go",
			desc:    "inventory-source sync (no template)",
			columns: []string{"name", "status", "created_at", "job_args"},
			jobArgs: true,
		},
		{
			file:    "services/scheduler/core/triggers.go",
			desc:    "schedule / event-trigger launch — DROPS job_args (bug #79)",
			columns: []string{"name", "unified_job_template_id", "status", "created_at"},
			jobArgs: false,
		},
		{
			file:    "services/scheduler/core/workflow.go",
			desc:    "workflow node launch — DROPS job_args (and created_at)",
			columns: []string{"name", "unified_job_template_id", "status"},
			jobArgs: false,
		},
	}

	for _, s := range surfaces {
		t.Run(s.file, func(t *testing.T) {
			cols := unifiedJobsInsertColumns(t, filepath.Join(root, s.file))
			if !equalStringSets(cols, s.columns) {
				t.Errorf("%s\n  %s\n  columns changed:\n    got:  %v\n    want: %v\n"+
					"If this is the pkg/launch extraction (B2), update the frozen contract in this test.",
					s.file, s.desc, cols, s.columns)
			}
			if hasJobArgs := contains(cols, "job_args"); hasJobArgs != s.jobArgs {
				t.Errorf("%s: job_args presence changed: got %v want %v (%s)", s.file, hasJobArgs, s.jobArgs, s.desc)
			}
		})
	}
}

var unifiedJobsInsertRe = regexp.MustCompile(`INSERT INTO unified_jobs\s*\(([^)]*)\)`)

// unifiedJobsInsertColumns extracts the column list of the single
// `INSERT INTO unified_jobs (...)` statement in a source file.
func unifiedJobsInsertColumns(t *testing.T, path string) []string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := unifiedJobsInsertRe.FindAllStringSubmatch(string(src), -1)
	if len(m) != 1 {
		t.Fatalf("%s: expected exactly one `INSERT INTO unified_jobs`, found %d", path, len(m))
	}
	var cols []string
	for _, c := range strings.Split(m[0][1], ",") {
		if c = strings.TrimSpace(c); c != "" {
			cols = append(cols, c)
		}
	}
	return cols
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

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
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
