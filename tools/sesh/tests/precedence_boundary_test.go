package tests

// U10 boundary guard (R15, spec §3.2, captures Lane 3 settled decision):
// display-owner precedence is view-time store/surface logic, revisable
// without touching any node. No precedence logic may migrate into the
// shipper — the shipper observes and ships SESSION_OWNER facts (U9), it
// never ranks, resolves, or displays them.

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

// precedenceMarkers match identifiers/vocabulary that only exist on the
// view-time side. SESSION_OWNER itself is deliberately NOT a marker: the
// shipper legitimately observes and ships it.
var precedenceMarkers = regexp.MustCompile(`(?i)display_?owner|precedence|conflicting[_ ]?claims`)

func TestShipperCarriesNoPrecedenceLogic(t *testing.T) {
	shipDir := filepath.Join(moduleRoot(t), "internal", "ship")
	fset := token.NewFileSet()
	err := filepath.WalkDir(shipDir, func(path string, d fs.DirEntry, err error) error {
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
			if ipath == "sesh/internal/surface" {
				t.Errorf("%s imports sesh/internal/surface: display-owner logic must stay store/surface-side", path)
			}
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := precedenceMarkers.Find(src); m != nil {
			t.Errorf("%s contains precedence vocabulary %q: owner precedence is view-time store/surface logic only", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
