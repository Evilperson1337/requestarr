package secrets_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// nameAllowed lists the only production files that may name a stored secret
// (secrets.TMDBReadToken and the like): the wiring that resolves, seeds and
// checks each one, and the one store that owns OIDC. A fact about a secret
// (its label, env var, or "another screen manages it") belongs in the registry
// as a Spec field, and a caller reads it with secrets.Lookup. Widening this
// list needs a reason in the diff.
var nameAllowed = map[string]bool{
	"app/secrets.go":                  true, // secretFields: Config field per secret
	"app/runtime.go":                  true,
	"app/providers.go":                true,
	"setup/targets.go":                true, // credential env names per target
	"setup/checks/messages.go":        true,
	"setup/checks/checker.go":         true, // resolver call per secret
	"store/postgres/oidc_settings.go": true, // the OIDC secret's own row
	"authsettings/service.go":         true, // opens and seals the OIDC secret
}

func TestSecretNamesAreSpelledOnlyInAllowedFiles(t *testing.T) {
	constants := secretConstants(t)
	root := ".."
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if nameAllowed[rel] || strings.HasPrefix(rel, "secrets/") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "secrets" && constants[selector.Sel.Name] {
				t.Errorf("%s: secrets.%s outside the wiring; add a field to secrets.Spec and read it with secrets.Lookup", fset.Position(selector.Pos()), selector.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// secretConstants reads the identifiers of every Name constant out of the
// registry source, so a newly added secret is guarded without editing this test.
func secretConstants(t *testing.T) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "registry.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				found[name.Name] = true
			}
		}
	}
	if len(found) == 0 {
		t.Fatal("no secret constants found in registry.go")
	}
	return found
}
