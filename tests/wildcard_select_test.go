package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoWildcardSelectsRepoWide is the repo-wide CI gate for #39/#91: no
// `SELECT *` / `RETURNING *` may appear in a SQL string literal in any non-test
// .go file. Scanning `*` into a struct breaks ("missing destination name X") the
// instant the table gains a column — and on the dispatch path (the scheduler's
// schedule tick, pkg/inventoryrender's host/group render that ingestion runs at
// every job dispatch) that stops jobs from launching, not just one API read.
//
// The original #39 gate lived in services/api/handlers and only scanned that one
// package, which is exactly how two `SELECT *`s survived on the dispatch path
// until #91. This gate walks the whole tree so a new one can't hide in any
// package. New queries must use an explicit column list (store.XxxCols).
//
// It parses each file and inspects only string literals, so the patterns stay
// legal in comments (and in this test).
func TestNoWildcardSelectsRepoWide(t *testing.T) {
	root := repoRoot(t)
	banned := []string{"SELECT *", "RETURNING *"}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// Normalize whitespace so multi-line SQL ("SELECT\n\t*") is caught.
			val := strings.Join(strings.Fields(lit.Value), " ")
			for _, b := range banned {
				if strings.Contains(val, b) {
					pos := fset.Position(lit.Pos())
					t.Errorf("%s:%d: banned %q in SQL literal — use an explicit column list (store.XxxCols)",
						rel(root, path), pos.Line, b)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}
