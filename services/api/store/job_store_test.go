package store

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/praetordev/praetor/pkg/models"
)

// The store's column consts are hand-written and must stay in lockstep with the
// scanned struct's db tags: SELECTing a column the struct lacks (or vice versa)
// is exactly the "missing destination name" 500 the store exists to prevent.
// This asserts the invariant per domain so drift is caught at test time, not in
// production. Copy this table when adding a new domain's column const.
func TestColumnConstsMatchStructTags(t *testing.T) {
	cases := []struct {
		name string
		cols string
		typ  any
	}{
		{"unifiedJobCols", unifiedJobCols, models.UnifiedJob{}},
		{"executionRunCols", executionRunCols, models.ExecutionRun{}},
		{"jobEventCols", jobEventCols, models.JobEvent{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := dbTags(c.typ)
			got := splitCols(c.cols)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s columns drifted from %T db tags\n const: %v\n  tags: %v",
					c.name, c.typ, got, want)
			}
		})
	}
}

// dbTags returns the sorted set of `db:"..."` tags on a struct (ignoring "-").
func dbTags(v any) []string {
	rt := reflect.TypeOf(v)
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// splitCols parses a comma-separated column const into a sorted set.
func splitCols(cols string) []string {
	parts := strings.Split(cols, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
