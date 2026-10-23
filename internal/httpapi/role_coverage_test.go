package httpapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Source inspection needs no production observer, global route registry, or
// changes to the router. Unknown dynamic registrations fail closed so the guard
// cannot silently omit a new route expression it does not understand.
func mutationRegistrations(root string) (map[string]string, error) {
	found := make(map[string]string)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		err = inspectRoutePatterns(file, nil, func(pattern string, at token.Pos) error {
			method, route, explicit := strings.Cut(pattern, " ")
			if !explicit {
				// Existing API-prefix and SPA delegates are not leaf endpoints.
				if strings.HasSuffix(filepath.ToSlash(path), "/httpapi/router.go") && (pattern == "/api/v1/" || pattern == "/") {
					return nil
				}
				return fmt.Errorf("%s: methodless registration %q needs explicit methods and role-matrix coverage", fset.Position(at), pattern)
			}
			if method != "GET" && method != "HEAD" && method != "OPTIONS" {
				found[method+" "+strings.TrimPrefix(route, "/api/v1")] = fset.Position(at).String()
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("inspect registrations in %s: %w", path, err)
		}
		return nil
	})
	return found, err
}

func inspectRoutePatterns(node ast.Node, bindings map[string][]string, visit func(string, token.Pos) error) error {
	var failure error
	ast.Inspect(node, func(node ast.Node) bool {
		if failure != nil {
			return false
		}
		// A range binding is only a known set while it is not reassigned or
		// shadowed. Invalidate conservatively, even in conditional branches.
		switch statement := node.(type) {
		case *ast.AssignStmt:
			for index, lhs := range statement.Lhs {
				if name, ok := lhs.(*ast.Ident); ok {
					// Older Go loop-capture idiom: method := method.
					if len(statement.Rhs) == len(statement.Lhs) {
						if rhs, ok := statement.Rhs[index].(*ast.Ident); ok && rhs.Name == name.Name {
							continue
						}
					}
					delete(bindings, name.Name)
				}
			}
		case *ast.ValueSpec:
			for _, name := range statement.Names {
				delete(bindings, name.Name)
			}
		}
		if loop, ok := node.(*ast.RangeStmt); ok {
			local := make(map[string][]string, len(bindings)+1)
			for name, values := range bindings {
				local[name] = values
			}
			shadowed := make(map[string]bool)
			for _, variable := range []ast.Expr{loop.Key, loop.Value} {
				if name, ok := variable.(*ast.Ident); ok {
					delete(local, name.Name)
					if loop.Tok == token.DEFINE {
						shadowed[name.Name] = true
					} else {
						// Assignment-form ranges alter the enclosing binding.
						delete(bindings, name.Name)
					}
				}
			}
			if name, ok := loop.Value.(*ast.Ident); ok {
				if values, ok := routeStrings(loop.X, bindings); ok {
					local[name.Name] = values
				}
			}
			failure = inspectRoutePatterns(loop.Body, local, visit)
			// Assignments in nested loops can affect an enclosing variable.
			// Propagate unknown values, except for the loop's own declarations.
			for name := range bindings {
				if _, known := local[name]; !known && !shadowed[name] {
					delete(bindings, name)
				}
			}
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (method.Sel.Name != "HandleFunc" && method.Sel.Name != "Handle") {
			return true
		}
		if len(call.Args) == 0 {
			failure = fmt.Errorf("route registration has no pattern; extend the coverage guard")
			return false
		}
		patterns, ok := routeStrings(call.Args[0], bindings)
		if !ok {
			failure = fmt.Errorf("unresolved route pattern at token position %d; extend the coverage guard and role matrix", call.Pos())
			return false
		}
		for _, pattern := range patterns {
			if failure = visit(pattern, call.Pos()); failure != nil {
				return false
			}
		}
		return true
	})
	return failure
}

// Resolve only the literal, concatenation, and literal-range forms used by
// registrations today. Deliberately do not guess unsupported expressions.
func routeStrings(expr ast.Expr, bindings map[string][]string) ([]string, bool) {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind == token.STRING {
			value, err := strconv.Unquote(expr.Value)
			return []string{value}, err == nil
		}
	case *ast.Ident:
		values, ok := bindings[expr.Name]
		return values, ok
	case *ast.ParenExpr:
		return routeStrings(expr.X, bindings)
	case *ast.CompositeLit:
		var values []string
		for _, element := range expr.Elts {
			parts, ok := routeStrings(element, bindings)
			if !ok {
				return nil, false
			}
			values = append(values, parts...)
		}
		return values, true
	case *ast.BinaryExpr:
		if expr.Op != token.ADD {
			return nil, false
		}
		left, lOK := routeStrings(expr.X, bindings)
		right, rOK := routeStrings(expr.Y, bindings)
		if !lOK || !rOK {
			return nil, false
		}
		var values []string
		for _, l := range left {
			for _, r := range right {
				values = append(values, l+r)
			}
		}
		return values, true
	}
	return nil, false
}

func TestEveryMutatingEndpointHasARoleMatrixRow(t *testing.T) {
	registered, err := mutationRegistrations("..")
	if err != nil {
		t.Fatal(err)
	}
	covered := make(map[string]bool)
	for _, endpoint := range mutationCases() {
		covered[endpoint.pattern] = true
		if registered[endpoint.pattern] == "" {
			t.Errorf("matrix row %q no longer matches a production registration", endpoint.pattern)
		}
	}
	for pattern, where := range registered {
		if !covered[pattern] {
			t.Errorf("%s: mutating endpoint %q has no four-role matrix row", where, pattern)
		}
	}
}

func TestRouteCoverageGuardExpandsLoopsAndFlagsUnknownRegistrations(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []string
		failure      string
	}{
		{"literal new mutation", `package example; func register() { mux.HandleFunc("PATCH /future", handler) }`, []string{"PATCH /future"}, ""},
		{"method loop", `package example; func register() { for _, method := range []string{"PUT","DELETE"} { mux.HandleFunc(method+" /future", handler) } }`, []string{"PUT /future", "DELETE /future"}, ""},
		{"captured method loop", `package example; func register() { for _, method := range []string{"PUT","DELETE"} { method := method; mux.HandleFunc(method+" /future", handler) } }`, []string{"PUT /future", "DELETE /future"}, ""},
		{"action loop", `package example; func register() { for _, action := range []string{"accept","dismiss"} { mux.HandleFunc("POST /future/"+action, handler) } }`, []string{"POST /future/accept", "POST /future/dismiss"}, ""},
		{"safe method excluded", `package example; func register() { mux.HandleFunc("GET /future", handler) }`, nil, ""},
		{"dynamic pattern flagged", `package example; func register() { mux.HandleFunc(makePattern(), handler) }`, nil, "unresolved route pattern"},
		{"reassigned loop variable flagged", `package example; func register() { for _, method := range []string{"PUT"} { method="PATCH"; mux.HandleFunc(method+" /future", handler) } }`, nil, "unresolved route pattern"},
		{"shadowed loop variable flagged", `package example; func register() { for _, method := range []string{"PUT"} { method := customMethod(); mux.HandleFunc(method+" /future", handler) } }`, nil, "unresolved route pattern"},
		{"nested assignment flagged", `package example; func register() { for _, method := range []string{"PUT"} { for _, other := range []string{"nested"} { method="PATCH" }; mux.HandleFunc(method+" /future", handler) } }`, nil, "unresolved route pattern"},
		{"range key shadow flagged", `package example; func register() { for _, method := range []string{"PUT"} { for method := range []string{"nested"} { mux.HandleFunc(method+" /future", handler) } } }`, nil, "unresolved route pattern"},
		{"assignment range invalidates outer binding", `package example; func register() { for _, method := range []string{"PUT"} { for _, method = range []string{"PATCH"} {}; mux.HandleFunc(method+" /future", handler) } }`, nil, "unresolved route pattern"},
		{"nested declaration leaves outer value known", `package example; func register() { for _, method := range []string{"PUT"} { for _, method := range []string{"DELETE"} { mux.HandleFunc(method+" /inner", handler) }; mux.HandleFunc(method+" /outer", handler) } }`, []string{"DELETE /inner", "PUT /outer"}, ""},
		{"methodless leaf flagged", `package example; func register() { mux.HandleFunc("/future", handler) }`, nil, "methodless registration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "routes.go"), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			found, err := mutationRegistrations(root)
			if tc.failure != "" {
				if err == nil || !strings.Contains(err.Error(), tc.failure) {
					t.Fatalf("unsupported registration was not flagged: %v", err)
				}
				return
			}
			if err != nil || len(found) != len(tc.want) {
				t.Fatalf("inventory=%v want %v err=%v", found, tc.want, err)
			}
			for _, pattern := range tc.want {
				if found[pattern] == "" {
					t.Errorf("new mutation %q was not detected", pattern)
				}
			}
		})
	}
}
