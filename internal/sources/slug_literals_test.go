package sources_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/sources"
)

// slugAllowed lists the only production files that may spell a Source kind
// slug as a string literal: the registry, each adapter's descriptor, and the
// accessors that must name a struct field or a provider client per kind.
var slugAllowed = map[string]bool{
	"sources/sources.go":                 true,
	"inventory/jellyfin/client.go":       true,
	"inventory/plex/client.go":           true,
	"inventory/romm/client.go":           true,
	"inventory/navidrome/client.go":      true,
	"inventory/audiobookshelf/client.go": true,
	"config/config.go":                   true, // libraryFields
	"setup/targets.go":                   true, // LibrariesSettings.Source, credential env names
	"app/app.go":                         true, // newInventorySource
	"setup/checks/checker.go":            true, // libraryProbes
}

// Adding a Source kind should mean editing the registry, not hunting for
// switches. Any other per-kind behaviour belongs in internal/sources.
func TestSourceSlugsAreSpelledOnlyInAllowedFiles(t *testing.T) {
	slugs := map[string]bool{}
	for _, k := range sources.All() {
		slugs[k.Slug] = true
	}
	root := ".."
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if slugAllowed[rel] {
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
			if value, err := strconv.Unquote(lit.Value); err == nil && slugs[value] {
				t.Errorf("%s: Source kind slug %s outside the registry; read it from internal/sources", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
