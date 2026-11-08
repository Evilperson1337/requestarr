package guardrails_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Settings, Logs shows superusers every record the server logs (D042), with
// no redaction of its own: what keeps credentials and personal data off that
// page is that they are never logged. This test finds every log field whose
// name suggests such a value.

// sensitiveFieldWords mark a log field name that suggests a credential,
// personal data or what a person typed.
var sensitiveFieldWords = []string{"password", "passwd", "token", "secret", "credential", "cookie", "authorization", "apikey", "api_key", "groups", "email", "query", "search"}

// logFieldAllowed lists the sensitive-looking field names that are safe, by
// file, with why. Widening it needs the reason in the diff. stdoutOnly means
// every call logging the field must pass a logbuffer.StdoutOnly context, so
// the record reaches the console and never Settings, Logs.
var logFieldAllowed = map[string]struct {
	files      string // space separated, relative to internal/
	stdoutOnly bool
	why        string
}{
	"one_time_password":  {"auth/bootstrap.go", true, "the first-run password, printed once to the console by design (D040)"},
	"secret":             {"app/connections_service.go app/secrets.go", false, "the stored secret's name, never its value"},
	"groups_claim":       {"auth/handler.go", false, "the configured claim name, not anyone's groups"},
	"groups_claim_found": {"auth/handler.go", false, "whether the claim was present"},
	"token_claims":       {"auth/handler.go", false, "the ID token's claim names, never their values"},
}

// logMethods maps a logging method to the index of its first key-value arg.
var logMethods = map[string]int{
	"Debug": 1, "Info": 1, "Warn": 1, "Error": 1,
	"DebugContext": 2, "InfoContext": 2, "WarnContext": 2, "ErrorContext": 2,
	"Log": 3, "With": 0,
	"log": 3, // auth.Handler's own wrapper: (ctx, level, msg, args...)
}

// attrConstructors build one slog attribute from a key and a value.
var attrConstructors = map[string]bool{"String": true, "Int": true, "Int64": true, "Bool": true, "Any": true, "Duration": true, "Time": true, "Float64": true, "Uint64": true, "Group": true}

func TestNoLogFieldCarriesACredentialOrPersonalData(t *testing.T) {
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel("..", path)
			rel = filepath.ToSlash(rel)
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				check := func(key string) {
					if !sensitive(key) {
						return
					}
					allowed, ok := logFieldAllowed[key]
					switch {
					case !ok || !strings.Contains(" "+allowed.files+" ", " "+rel+" "):
						t.Errorf("%s: log field %q looks like a credential or personal data, and Settings, Logs shows every field; log something safe instead, or add it to logFieldAllowed with the reason", fset.Position(call.Pos()), key)
					case allowed.stdoutOnly && !passesStdoutOnly(call):
						t.Errorf("%s: log field %q must be logged with a logbuffer.StdoutOnly context so Settings, Logs never shows it", fset.Position(call.Pos()), key)
					}
				}
				if first, ok := logMethods[sel.Sel.Name]; ok {
					for _, key := range keysFrom(call.Args, first) {
						check(key)
					}
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "slog" && attrConstructors[sel.Sel.Name] && len(call.Args) > 0 {
					if key, ok := stringLit(call.Args[0]); ok {
						check(key)
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
}

// keysFrom returns the literal keys of key-value args starting at first,
// looking inside append(base, k, v, ...) as the auth handler builds its fields.
func keysFrom(args []ast.Expr, first int) []string {
	var keys []string
	for i := first; i < len(args); i++ {
		if call, ok := args[i].(*ast.CallExpr); ok {
			if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "append" {
				keys = append(keys, keysFrom(call.Args, 1)...)
				continue
			}
		}
		if (i-first)%2 == 0 {
			if key, ok := stringLit(args[i]); ok {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func sensitive(key string) bool {
	key = strings.ToLower(key)
	for _, word := range sensitiveFieldWords {
		if strings.Contains(key, word) {
			return true
		}
	}
	return false
}

// passesStdoutOnly reports whether a *Context logging call's context argument
// is logbuffer.StdoutOnly(...).
func passesStdoutOnly(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	ctx, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := ctx.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "StdoutOnly"
}
