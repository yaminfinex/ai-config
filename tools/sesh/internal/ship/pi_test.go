package ship

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sesh/internal/wire"
)

const (
	piSID    = "019f64a0-1111-7222-8333-444444444444"
	piCwdKey = "--workspace-pi-fixture--"
)

func writePiAgent(t *testing.T, base string, transcript []byte) (string, []string) {
	t.Helper()
	agent := filepath.Join(base, ".pi", "agent")
	write := func(rel, content string) string {
		t.Helper()
		path := filepath.Join(agent, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	forbidden := []string{
		write("auth.json", `{"fixture_secret":"must-not-ship"}`),
		write("settings.json", `{}`), write("models.json", `{}`),
		write("extensions/hcom.ts", "// runtime extension"),
		write("logs/debug.jsonl", `{"runtime":true}`+"\n"),
		write("sessions/session_search.sqlite", "sqlite"),
		write("sessions/"+piCwdKey+"/notes.jsonl", `{"decoy":true}`+"\n"),
		write("sessions/"+piCwdKey+"/nested/2026-07-15T12-34-56-789Z_"+piSID+".jsonl", `{"decoy":true}`+"\n"),
		write("sessions/"+piCwdKey+"/2026-07-15_"+piSID+".jsonl", `{"decoy":true}`+"\n"),
	}
	real := filepath.Join(agent, "sessions", piCwdKey, "2026-07-15T12-34-56-789Z_"+piSID+".jsonl")
	if err := os.WriteFile(real, transcript, 0o600); err != nil {
		t.Fatal(err)
	}
	outside := write("outside/2026-07-15T12-34-56-789Z_"+piSID+".jsonl", "outside")
	forbidden = append(forbidden, outside)
	if err := os.Symlink(outside, filepath.Join(agent, "sessions", piCwdKey, "2026-07-15T12-34-57-000Z_"+piSID+".jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(agent, "sessions", "--symlinked--")); err != nil {
		t.Fatal(err)
	}
	return agent, forbidden
}

func assertPiBoundary(t *testing.T, root string, discovered []Discovered, forbidden []string) int {
	t.Helper()
	bad := map[string]bool{}
	for _, path := range forbidden {
		bad[path] = true
	}
	violations := 0
	for _, item := range discovered {
		rel, err := filepath.Rel(root, item.Path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if err != nil || bad[item.Path] || len(parts) != 2 || parts[0] == ".." || piName.FindStringSubmatch(parts[1]) == nil {
			violations++
		}
	}
	return violations
}

func TestPiDiscoveryExactShapeAndIdentity(t *testing.T) {
	agent, forbidden := writePiAgent(t, t.TempDir(), fixture(t, "pi-branched-session.jsonl"))
	root := filepath.Join(agent, "sessions")
	got, err := Discover(Roots{Pi: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Identity != (Identity{Tool: wire.ToolPi, SessionID: piSID, FileUUID: piSID}) {
		t.Fatalf("pi discovery = %+v", got)
	}
	if n := assertPiBoundary(t, root, got, forbidden); n != 0 {
		t.Fatalf("pi exclusion boundary violations = %d", n)
	}
	if _, ok := piMatch("../outside/2026-07-15T12-34-56-789Z_"+piSID+".jsonl", fakeDirEntry{}); ok {
		t.Fatal("pi matcher admitted traversal-shaped relative path")
	}
}

func TestPiBoundaryDetectorProven(t *testing.T) {
	agent, forbidden := writePiAgent(t, t.TempDir(), fixture(t, "pi-branched-session.jsonl"))
	sessions := filepath.Join(agent, "sessions")
	widened := map[string]struct {
		root  string
		match func(string, fs.DirEntry) (string, bool)
	}{
		"any-jsonl": {sessions, func(_ string, d fs.DirEntry) (string, bool) { return piSID, strings.HasSuffix(d.Name(), ".jsonl") }},
		"any-depth-valid-name": {sessions, func(_ string, d fs.DirEntry) (string, bool) {
			m := piName.FindStringSubmatch(d.Name())
			if m != nil {
				return m[1], true
			}
			return "", false
		}},
		"whole-agent": {agent, func(string, fs.DirEntry) (string, bool) { return piSID, true }},
	}
	for name, test := range widened {
		got, err := walkRoot(test.root, wire.ToolPi, test.match)
		if err != nil {
			t.Fatal(err)
		}
		if assertPiBoundary(t, sessions, got, forbidden) == 0 {
			t.Fatalf("widened matcher %q did not trip the boundary detector", name)
		}
	}
}

func TestPiDiscoveryRejectsSymlinkedSessionRoot(t *testing.T) {
	base := t.TempDir()
	agent := filepath.Join(base, ".pi", "agent")
	outRoot := filepath.Join(base, "outside")
	outside := filepath.Join(outRoot, piCwdKey, "2026-07-15T12-34-56-789Z_"+piSID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, fixture(t, "pi-branched-session.jsonl"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agent, 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(agent, "sessions")
	if err := os.Symlink(outRoot, root); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(Roots{Pi: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Pi discovery followed a symlinked session root: %+v", got)
	}

	// Detector premise: the deliberately widened legacy policy follows the
	// root and must expose the outside file, proving this negative is live.
	mutant, err := walkRoot(root, wire.ToolPi, piMatch)
	if err != nil {
		t.Fatal(err)
	}
	if len(mutant) != 1 || mutant[0].Path != outside {
		t.Fatalf("root-policy mutant did not trip detector: %+v", mutant)
	}
}

func TestPiBackfillShipsFixture(t *testing.T) {
	h := newHarness(t)
	raw := fixture(t, "pi-branched-session.jsonl")
	h.writePi(piCwdKey, piSID, raw)
	h.runOnce()
	h.assertMirror("pi", piSID, raw)
	if cursor, ok := h.cursor(wire.ToolPi, piSID); !ok || cursor.Offset != int64(len(raw)) {
		t.Fatalf("pi cursor = %+v, ok=%v", cursor, ok)
	}
}

func TestPreAmendmentStoreParksPiWithoutBlockingOthers(t *testing.T) {
	h := newHarness(t)
	h.store.preAmendment4 = true
	claude := fixture(t, "claude-normal.jsonl")
	pi := fixture(t, "pi-branched-session.jsonl")
	h.writeClaude("-project", uuidNormal, claude)
	h.writePi(piCwdKey, piSID, pi)
	h.runOnce()
	h.runOnce()
	h.assertMirror("claude", uuidNormal, claude)
	if puts := h.store.puts("pi", piSID, piSID); len(puts) != 0 {
		t.Fatalf("pre-amendment store received Pi PUTs: %v", puts)
	}
	if _, ok := h.cursor(wire.ToolPi, piSID); ok {
		t.Fatal("pre-amendment Pi refusal advanced a cursor")
	}
	h.store.preAmendment4 = false
	h.restart()
	h.runOnce()
	h.assertMirror("pi", piSID, pi)
}

// fakeDirEntry is sufficient for direct matcher traversal-negative checks.
type fakeDirEntry struct{}

func (fakeDirEntry) Name() string               { return "fixture" }
func (fakeDirEntry) IsDir() bool                { return false }
func (fakeDirEntry) Type() fs.FileMode          { return 0 }
func (fakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }
