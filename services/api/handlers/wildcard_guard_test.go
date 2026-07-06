package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoWildcardSelects is the CI gate for #39: no `SELECT *` / `RETURNING *`
// may live in a SQL string literal in this package. Scanning into a struct with
// `*` breaks ("missing destination name X") the moment the table gains a column
// — the exact class of live 500 this sweep removed. New queries must reference an
// explicit column list from the store package (store.XxxCols).
//
// It parses each source file and inspects only string literals, so the patterns
// remain legal in comments (and in this test).
func TestNoWildcardSelects(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{"SELECT *", "RETURNING *"}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// Normalize whitespace so multi-line SQL ("RETURNING\n\t*") is caught.
			val := strings.Join(strings.Fields(lit.Value), " ")
			for _, b := range banned {
				if strings.Contains(val, b) {
					pos := fset.Position(lit.Pos())
					t.Errorf("%s:%d: banned %q in SQL literal — use an explicit store.XxxCols list",
						f, pos.Line, b)
				}
			}
			return true
		})
	}
}
