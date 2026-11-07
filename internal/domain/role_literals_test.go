package domain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var roleNames = map[string]bool{"viewer": true, "requester": true, "admin": true, "superuser": true}

// roleLiteralAllowed lists the only production Go files that may spell a role
// name as a string literal: the role module, and files where the same word
// means something else.
var roleLiteralAllowed = map[string]string{
	"domain/role.go":    "the role module",
	"domain/request.go": "SubscriptionRequester is a subscription source, not a role",
	"auth/local.go":     "LocalUsername is the sign-in name of the built-in account",
}

// A role is asked what it may do; nobody compares its name. Everything that
// decides access goes through internal/domain/role.go (or, in the web app,
// through the capabilities the session returns).
func TestRoleNamesAreSpelledOnlyInTheRoleModule(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if _, ok := roleLiteralAllowed[filepath.ToSlash(rel)]; ok {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if roleNames[value] || sqlRoleComparison.MatchString(value) {
				t.Errorf("%s: role name %s outside internal/domain/role.go; ask the role what it may do", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// role='admin', u.role <> 'viewer', role IN ('admin', ...) inside SQL text.
var sqlRoleComparison = regexp.MustCompile(`(?i)\brole\s*(=|<>|!=|in\b)\s*\(?\s*'(viewer|requester|admin|superuser)'`)

// webRoleComparison finds role === 'admin', 'admin' === role, role !== "viewer",
// and the same on a property such as session.user.role.
var webRoleComparison = regexp.MustCompile(`(?i)\brole\s*[!=]==?\s*['"](viewer|requester|admin|superuser)['"]|['"](viewer|requester|admin|superuser)['"]\s*[!=]==?\s*[\w.?]*\brole\b`)

func TestWebAppNeverComparesRoleNames(t *testing.T) {
	root := filepath.Join("..", "..", "web", "src")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if loc := webRoleComparison.FindString(line); loc != "" {
				t.Errorf("%s:%d: compares a role name (%s); use session.capabilities", path, i+1, strings.TrimSpace(loc))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
