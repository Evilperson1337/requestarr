// Package guardrails holds repository-wide checks that no single package owns.
package guardrails_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// errorTextAllowed lists the production files that may inspect an error's text
// with the strings package. A driver or library error's text can echo input,
// credentials included, and changes between versions, so a cause is chosen from
// the error's type (errors.Is and errors.As). Widening this list needs a reason
// in the diff.
var errorTextAllowed = map[string]bool{
	"store/postgres/pool.go":              true, // TLS failures pgx reports only as fixed text; see isTLSProblem
	"providers/metadata/igdb/resolver.go": true, // retries once on the provider's "401" text
	"inventory/inventorytest/suite.go":    true, // asserts a canary secret is absent from an error
}

func TestErrorTextIsInspectedOnlyInAllowedFiles(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if errorTextAllowed[filepath.ToSlash(rel)] {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isStringsCall(call) {
				return true
			}
			for _, argument := range call.Args {
				if mentionsErrorText(argument) {
					t.Errorf("%s: strings call on an error's text; choose the cause with errors.Is or errors.As", fset.Position(call.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isStringsCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "strings"
}

// mentionsErrorText reports whether the expression contains a call such as
// err.Error() anywhere inside it (for example strings.ToLower(err.Error())).
func mentionsErrorText(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Error" {
			found = true
		}
		return !found
	})
	return found
}
