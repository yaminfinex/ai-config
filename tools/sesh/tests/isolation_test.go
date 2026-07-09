// Package tests holds module-level guard tests and the scenario fixture
// corpus. The isolation test enforces the standalone-module ruling: sesh
// imports nothing from the host repo and builds from this directory alone.
package tests

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// hostRepoMarkers are path fragments that would indicate a dependency on the
// repository currently hosting the module. The module must stay portable.
var hostRepoMarkers = []string{"ai-config", "herdr"}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGoModHasNoHostRepoPathsOrReplaces(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	gomod := string(raw)
	if strings.Contains(gomod, "replace ") || strings.Contains(gomod, "replace(") {
		t.Error("go.mod contains a replace directive; the module must build from the public proxy alone")
	}
	for _, marker := range hostRepoMarkers {
		if strings.Contains(strings.ToLower(gomod), marker) {
			t.Errorf("go.mod references host repo marker %q", marker)
		}
	}
}

func TestImportsAreStdlibModuleLocalOrDeclared(t *testing.T) {
	root := moduleRoot(t)
	declared := declaredRequires(t, root)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			ipath := strings.Trim(imp.Path.Value, `"`)
			if !importAllowed(ipath, declared) {
				t.Errorf("%s imports %q: not stdlib, not module-local, not declared in go.mod", path, ipath)
			}
			for _, marker := range hostRepoMarkers {
				if strings.Contains(strings.ToLower(ipath), marker) {
					t.Errorf("%s imports %q: references host repo marker %q", path, ipath, marker)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func importAllowed(ipath string, declared map[string]bool) bool {
	if ipath == "sesh" || strings.HasPrefix(ipath, "sesh/") {
		return true
	}
	first, _, _ := strings.Cut(ipath, "/")
	if !strings.Contains(first, ".") {
		return true // stdlib by convention: no dot in the first path segment
	}
	for mod := range declared {
		if ipath == mod || strings.HasPrefix(ipath, mod+"/") {
			return true
		}
	}
	return false
}

var requireLine = regexp.MustCompile(`(?m)^\s*(?:require\s+)?([a-z0-9.\-/]+\.[a-z][a-z0-9.\-/]*)\s+v\S+`)

func declaredRequires(t *testing.T, root string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	mods := map[string]bool{}
	for _, m := range requireLine.FindAllStringSubmatch(string(raw), -1) {
		mods[m[1]] = true
	}
	return mods
}
