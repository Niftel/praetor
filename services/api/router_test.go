package api

import (
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // register the "postgres" driver for the lazy sql.Open below
)

var updateGolden = flag.Bool("update", false, "update golden files")

func wantUpdate() bool { return *updateGolden || os.Getenv("UPDATE_GOLDEN") == "1" }

// TestRouterTableGolden freezes the full HTTP surface (method + path pattern) of
// the API router. Route wiring is deliberately explicit and greppable in
// router.go; this test makes any accidental addition, removal, or path change
// visible in review as a golden-file diff.
//
// It is a guard rail for the ContentHandler dissolution (B6): mechanically
// splitting the god-object's inline routes into per-domain Resources must not
// alter the route table. See docs/coupling-decomposition-plan.md (B1, B6).
//
// NewRouter only registers handlers; it never touches the database at
// construction time, so an unconnected *sqlx.DB is sufficient to walk the tree.
func TestRouterTableGolden(t *testing.T) {
	// sql.Open is lazy: it validates the driver and stores the DSN but never
	// dials, so this DB is safe to hand to NewRouter for route registration.
	sqlDB, err := sql.Open("postgres", "")
	if err != nil {
		t.Fatalf("open db handle: %v", err)
	}
	db := sqlx.NewDb(sqlDB, "postgres")
	t.Cleanup(func() { _ = db.Close() })

	r, err := NewRouter(db, Config{})
	if err != nil {
		t.Fatal(err)
	}

	var lines []string
	err = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi appends a trailing slash to mount points; normalise for stability.
		route = strings.ReplaceAll(route, "/*/", "/")
		lines = append(lines, fmt.Sprintf("%-7s %s", method, route))
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	sort.Strings(lines)
	got := strings.Join(lines, "\n") + "\n"

	golden := filepath.Join("testdata", "routes.golden")
	if wantUpdate() {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s (%d routes)", golden, len(lines))
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("API route table drifted.\n"+
			"If intentional, regenerate with:  go test ./services/api -run RouterTable -update\n\n--- got ---\n%s", got)
	}
}

func TestNewRouterRejectsUnsafeIngestionURL(t *testing.T) {
	if _, err := NewRouter(nil, Config{IngestionURL: "file:///etc/passwd"}); err == nil {
		t.Fatal("NewRouter accepted an unsafe ingestion URL")
	}
}
