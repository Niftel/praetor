package store

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/praetordev/models"
)

// Every column const must stay in lockstep with the db tags of the struct it is
// scanned into: SELECTing a column the struct lacks (or omitting one it has) is
// exactly the "missing destination name" 500 the explicit lists exist to prevent.
// This asserts the invariant for every domain so drift is caught at test time,
// not in production. Add a row here whenever a new column const is introduced.
func TestColumnConstsMatchStructTags(t *testing.T) {
	cases := []struct {
		name string
		cols string
		typ  any
	}{
		// jobs domain
		{"unifiedJobCols", unifiedJobCols, models.UnifiedJob{}},
		{"executionRunCols", executionRunCols, models.ExecutionRun{}},
		{"jobEventCols", jobEventCols, models.JobEvent{}},
		// resource domains
		{"CredentialCols", CredentialCols, models.Credential{}},
		{"CredentialTypeCols", CredentialTypeCols, models.CredentialType{}},
		{"HostCols", HostCols, models.Host{}},
		{"InventoryCols", InventoryCols, models.Inventory{}},
		{"GroupCols", GroupCols, models.Group{}},
		{"JobTemplateCols", JobTemplateCols, models.JobTemplate{}},
		{"ProjectCols", ProjectCols, models.Project{}},
		{"OrganizationCols", OrganizationCols, models.Organization{}},
		{"TeamCols", TeamCols, models.Team{}},
		{"ScheduleCols", ScheduleCols, models.Schedule{}},
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
