package ship

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// forEachFixtureRoot runs path-sensitive discovery tests once through a
// canonical fixture spelling and once through a symlinked TMPDIR spelling.
// The latter keeps Linux CI sensitive to the /var -> /private/var spelling
// difference that is always present on macOS.
func forEachFixtureRoot(t *testing.T, test func(t *testing.T, base string)) {
	t.Helper()
	t.Run("canonical", func(t *testing.T) {
		test(t, canonicalPath(t, t.TempDir()))
	})
	t.Run("symlinked-TMPDIR", func(t *testing.T) {
		real := canonicalPath(t, t.TempDir())
		linked := real + "-link"
		if err := os.Symlink(real, linked); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(linked) })
		t.Setenv("TMPDIR", linked)
		base, err := os.MkdirTemp("", "sesh-fixture-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(base) })
		resolved := canonicalPath(t, base)
		if resolved == base {
			t.Fatalf("fixture does not exercise distinct path spellings: %s", base)
		}
		test(t, base)
	})
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve fixture path %q: %v", path, err)
	}
	return resolved
}

func canonicalPaths(t *testing.T, paths []string) []string {
	t.Helper()
	resolved := make([]string, len(paths))
	for i, path := range paths {
		resolved[i] = canonicalPath(t, path)
	}
	return resolved
}

// assertDiscoveredPaths pins the production walker's exact output on the
// canonical fixture. Canonicalizing only the expected fixture spellings must
// not widen discovery or otherwise change the emitted set.
func assertDiscoveredPaths(t *testing.T, discovered []Discovered, want []string) {
	t.Helper()
	got := make([]string, len(discovered))
	for i, item := range discovered {
		got[i] = item.Path
	}
	want = canonicalPaths(t, want)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("discovered paths = %q, want byte-identical set %q", got, want)
	}
}
