package handlers

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// TestDiagnosticQueriesHaveBoundedIndexPlans proves the three supported page
// shapes can use their purpose-built indexes. CI runs this against the fully
// migrated PostgreSQL schema; local infra-free runs skip it.
func TestDiagnosticQueriesHaveBoundedIndexPlans(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sqlx.Connect("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tests := []struct {
		name, statement, index string
	}{
		{"sequence", `SELECT seq FROM job_events WHERE execution_run_id='00000000-0000-0000-0000-000000000001' AND seq>0 ORDER BY seq LIMIT 101`, "job_events_execution_run_id_seq_key"},
		{"outcome", `SELECT seq FROM job_events WHERE execution_run_id='00000000-0000-0000-0000-000000000001' AND diagnostic_outcome='failed' AND seq>0 ORDER BY seq LIMIT 101`, "idx_job_events_run_outcome_seq"},
		{"event type", `SELECT seq FROM job_events WHERE execution_run_id='00000000-0000-0000-0000-000000000001' AND event_type='HOST_FAILED' AND seq>0 ORDER BY seq LIMIT 101`, "idx_job_events_run_type_seq"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx, err := db.BeginTxx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.Exec(`SET LOCAL enable_seqscan = off`); err != nil {
				t.Fatal(err)
			}
			rows, err := tx.Queryx(`EXPLAIN (COSTS OFF) ` + test.statement)
			if err != nil {
				t.Fatal(err)
			}
			var plan strings.Builder
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Fatal(err)
				}
				plan.WriteString(line)
				plan.WriteByte('\n')
			}
			_ = rows.Close()
			if !strings.Contains(plan.String(), test.index) {
				t.Fatalf("bounded diagnostic query did not use %s:\n%s", test.index, plan.String())
			}
		})
	}
}
