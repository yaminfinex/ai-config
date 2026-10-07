package servecmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The raw roster function asks hcom (`hcom list`, 0.2-0.5 s) on every call.
// Only the pollers that feed the roster cache, and the liveFleet service
// handlers read through, may touch it. A source walk rather than a structural
// wall: deps.roster is a test seam set by dozens of fixtures, and request
// handlers receive the same dependencies value the pollers do, so hiding the
// field would mean splitting that struct across every test. The walk is
// exact instead: it names each allowed function, fails on a new one, and
// fails on an allowlisted name that no longer exists, so the list cannot rot.
var rawRosterAllowed = map[string]string{
	"liveDependencies":   "wires hcomidentity.List as the production seam",
	"dependencies.fleet": "hands the seam to the service",
	"liveFleet.Poll":     "the service's one live ask, which stores into the cache",
	"cachingRoster":      "the observer poll: a cache refresher",
	"startLifeMirror":    "background life mirror: stamps ready from a roster of its own moment",
	"sweepStateOnce":     "background state sweep: its absence rule deletes state, so it reads live",
}

// liveFleet.Poll skips the cache. Off the service, only a cache refresher or
// a miss path that has already consulted the cache may call it.
var livePollAllowed = map[string]string{
	"readFleetInputs":      "the SSE board poll passes live; a GET reads the cache",
	"resolveAgentEvidence": "miss path after a cached lookup lacked the name",
}

func TestRequestPathReachesHcomOnlyThroughLiveFleet(t *testing.T) {
	violations, seen := scanRosterReach(t, ".")
	for _, violation := range violations {
		t.Error(violation)
	}
	for name := range rawRosterAllowed {
		if !seen[name] {
			t.Errorf("allowlisted %s no longer reaches the raw roster; drop it from rawRosterAllowed", name)
		}
	}
	for name := range livePollAllowed {
		if !seen[name] {
			t.Errorf("allowlisted %s no longer calls liveFleet.Poll; drop it from livePollAllowed", name)
		}
	}
}

// scanRosterReach parses dir's non-test Go files and reports each reach for
// the raw roster (a .roster selector or hcomidentity.List) or liveFleet.Poll
// outside its allowlist; seen names the scopes that reached either.
func scanRosterReach(t *testing.T, dir string) ([]string, map[string]bool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var violations []string
	seen := map[string]bool{}
	scanned := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, path, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		for _, decl := range file.Decls {
			scope := declScope(decl)
			ast.Inspect(decl, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				where := fset.Position(selector.Pos())
				switch {
				case selector.Sel.Name == "roster" || isPackageSelector(selector, "hcomidentity", "List"):
					seen[scope] = true
					if _, ok := rawRosterAllowed[scope]; !ok {
						violations = append(violations, where.String()+": "+scope+" reaches the raw hcom roster; read it through deps.fleet() (liveFleet)")
					}
				case selector.Sel.Name == "Poll" && !strings.HasPrefix(scope, "liveFleet."):
					seen[scope] = true
					if _, ok := livePollAllowed[scope]; !ok {
						violations = append(violations, where.String()+": "+scope+" calls liveFleet.Poll, skipping the roster cache; use Roster or RosterHolding")
					}
				}
				return true
			})
		}
	}
	if scanned == 0 {
		t.Fatalf("no servecmd sources found in %s", dir)
	}
	slices.Sort(violations)
	return violations, seen
}

func isPackageSelector(selector *ast.SelectorExpr, pkg, name string) bool {
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == pkg && selector.Sel.Name == name
}

// declScope names a top-level declaration: "Func", "Recv.Method", or the
// first name of a var/const/type group.
func declScope(decl ast.Decl) string {
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		if decl.Recv == nil || len(decl.Recv.List) == 0 {
			return decl.Name.Name
		}
		receiver := decl.Recv.List[0].Type
		if star, ok := receiver.(*ast.StarExpr); ok {
			receiver = star.X
		}
		if ident, ok := receiver.(*ast.Ident); ok {
			return ident.Name + "." + decl.Name.Name
		}
		return decl.Name.Name
	case *ast.GenDecl:
		for _, spec := range decl.Specs {
			switch spec := spec.(type) {
			case *ast.ValueSpec:
				return spec.Names[0].Name
			case *ast.TypeSpec:
				return spec.Name.Name
			}
		}
	}
	return "<unknown>"
}
