package ship

// Grok adapter characterization: discovery shape, the exclusion boundary
// around ~/.grok (a security boundary — the top level holds config and
// credentials), and end-to-end shipping of the fixture transcript. The
// boundary detector is proven: the same walk with a deliberately widened
// matcher must trip the assertions.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sesh/internal/wire"
)

const (
	uuidGrokA = "019f5873-de81-7993-952f-ae68d3b6d703" // uuidv7 (live grok default)
	uuidGrokB = "71ebdd45-2641-49e8-87f5-b8d9f3706714" // uuidv4 (also observed live)
)

const grokCwdGroup = "%2Fhome%2Fuser%2Fproj"

// writeGrokHome builds a realistic ~/.grok: top-level config/credential/
// runtime decoys, two real session directories (each with the full
// non-transcript sibling set), and shape traps around the discovery glob.
// It returns the home directory and the set of paths that MUST NOT ship.
func writeGrokHome(t *testing.T, base string, transcript []byte) (home string, forbidden []string) {
	t.Helper()
	home = filepath.Join(base, "grok-home")
	write := func(rel, content string) string {
		t.Helper()
		p := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Top-level ~/.grok state: everything here is config, credentials, or
	// runtime bookkeeping and must never be discovered.
	forbidden = append(forbidden,
		write("config.toml", "api_key = \"grok-fake-key-must-never-ship\"\n"),
		write("active_sessions.json", `{"sessions":[]}`),
		write("agent_id", "agent-fake-id\n"),
		write("managed_config.lock", ""),
		write("models_cache.json", `{}`),
		write("CHANGELOG.md", "# changes\n"),
		write("logs/grok.log", "log line\n"),
		write("downloads/blob.bin", "bytes"),
		write("bin/grok", "#!/bin/sh\n"),
		write("completions/grok.bash", "complete -F _grok grok\n"),
		write("marketplace-cache/index.json", `{}`),
	)

	// Session directories: only chat_history.jsonl ships; every sibling is
	// runtime state.
	for _, sid := range []string{uuidGrokA, uuidGrokB} {
		dir := "sessions/" + grokCwdGroup + "/" + sid
		p := filepath.Join(home, filepath.FromSlash(dir), grokTranscriptName)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, transcript, 0o644); err != nil {
			t.Fatal(err)
		}
		forbidden = append(forbidden,
			write(dir+"/events.jsonl", `{"type":"turn_started","ts":"2026-07-09T00:00:00Z"}`+"\n"),
			write(dir+"/updates.jsonl", `{"seq":1}`+"\n"),
			write(dir+"/prompt_context.json", `{}`),
			write(dir+"/resources_state.json", `{}`),
			write(dir+"/rewind_points.jsonl", `{"prompt_index":0}`+"\n"),
			write(dir+"/signals.json", `{}`),
			write(dir+"/summary.json", `{}`),
			write(dir+"/system_prompt.txt", "system prompt text\n"),
			write(dir+"/recap_requests/recap-0.json", `{}`),
			write(dir+"/terminal/call-0.txt", "terminal output\n"),
			// Project-scope .grok/config.toml is config wherever it appears.
			write(dir+"/.grok/config.toml", "api_key = \"grok-fake-key-must-never-ship\"\n"),
			// Shape traps: the transcript name at the wrong depth, and a
			// session-shaped dir without a UUID name.
			write(dir+"/recap_requests/"+grokTranscriptName, `{"type":"user","content":"nested decoy"}`+"\n"),
		)
	}
	forbidden = append(forbidden,
		write("sessions/"+grokCwdGroup+"/"+grokTranscriptName, `{"type":"user","content":"too-shallow decoy"}`+"\n"),
		write("sessions/"+grokCwdGroup+"/not-a-uuid/"+grokTranscriptName, `{"type":"user","content":"non-uuid decoy"}`+"\n"),
		write("sessions/session_search.sqlite", "sqlite"),
	)
	return home, forbidden
}

// assertGrokBoundary is the exclusion detector: every discovered grok path
// must be exactly a UUID session directory's chat_history.jsonl, and no
// discovered path may be one of the forbidden fixtures.
func assertGrokBoundary(t *testing.T, discovered []Discovered, forbidden []string) (violations int) {
	t.Helper()
	forbiddenSet := map[string]bool{}
	for _, p := range forbidden {
		forbiddenSet[p] = true
	}
	for _, d := range discovered {
		if forbiddenSet[d.Path] {
			violations++
			continue
		}
		if filepath.Base(d.Path) != grokTranscriptName || !uuidName.MatchString(filepath.Base(filepath.Dir(d.Path))) {
			violations++
		}
	}
	return violations
}

func TestGrokDiscoveryShipsOnlySessionTranscripts(t *testing.T) {
	base := t.TempDir()
	transcript := fixture(t, "grok-chat-history.jsonl")
	home, forbidden := writeGrokHome(t, base, transcript)

	roots := Roots{Grok: filepath.Join(home, "sessions")}
	discovered, err := Discover(roots)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{uuidGrokA: false, uuidGrokB: false}
	if len(discovered) != len(want) {
		t.Fatalf("discovered %d files, want %d: %+v", len(discovered), len(want), discovered)
	}
	for _, d := range discovered {
		seen, ok := want[d.Identity.SessionID]
		if !ok || seen {
			t.Fatalf("unexpected or duplicate discovery: %+v", d)
		}
		want[d.Identity.SessionID] = true
		if d.Identity.Tool != wire.ToolGrok || d.Identity.FileUUID != d.Identity.SessionID {
			t.Fatalf("grok identity must be (grok, sid, sid): %+v", d.Identity)
		}
	}
	if v := assertGrokBoundary(t, discovered, forbidden); v != 0 {
		t.Fatalf("exclusion boundary violated by real discovery: %d violations", v)
	}
}

// TestGrokBoundaryDetectorProven drives the same walk with deliberately
// widened matchers — the drift classes a future edit could introduce — and
// requires the detector to fire for each. A detector that cannot see the
// widened globs would prove nothing (house rule: detectors get proven).
func TestGrokBoundaryDetectorProven(t *testing.T) {
	base := t.TempDir()
	home, forbidden := writeGrokHome(t, base, fixture(t, "grok-chat-history.jsonl"))

	widened := map[string]func(rel string, d fs.DirEntry) (string, bool){
		// Any *.jsonl under the sessions tree (events, updates, rewinds leak).
		"any-jsonl": func(rel string, d fs.DirEntry) (string, bool) {
			if strings.HasSuffix(d.Name(), ".jsonl") {
				return uuidGrokA, true
			}
			return "", false
		},
		// The right filename at any depth (nested recap decoy leaks).
		"any-depth-transcript": func(rel string, d fs.DirEntry) (string, bool) {
			if d.Name() == grokTranscriptName {
				return uuidGrokA, true
			}
			return "", false
		},
		// Everything (a root mistakenly widened to the whole home leaks
		// config.toml and credentials).
		"everything": func(rel string, d fs.DirEntry) (string, bool) {
			return uuidGrokA, true
		},
	}
	for name, match := range widened {
		root := filepath.Join(home, "sessions")
		if name == "everything" {
			root = home
		}
		discovered, err := walkRoot(root, wire.ToolGrok, match)
		if err != nil {
			t.Fatal(err)
		}
		if v := assertGrokBoundary(t, discovered, forbidden); v == 0 {
			t.Fatalf("widened glob %q did not trip the exclusion detector: the boundary test proves nothing", name)
		}
	}
}

// TestGrokBackfillShipsFixture ships the real grok fixture end to end through
// the wire (S1 shape for the third tool).
func TestGrokBackfillShipsFixture(t *testing.T) {
	h := newHarness(t)
	transcript := fixture(t, "grok-chat-history.jsonl")
	h.writeGrok(grokCwdGroup, uuidGrokB, transcript)
	// Sibling runtime state next to the transcript must not ship.
	sib := filepath.Join(h.roots.Grok, grokCwdGroup, uuidGrokB, "events.jsonl")
	if err := os.WriteFile(sib, []byte(`{"type":"turn_started"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.runOnce()

	h.assertMirror("grok", uuidGrokB, transcript)
	c, ok := h.cursor(wire.ToolGrok, uuidGrokB)
	if !ok || c.Offset != int64(len(transcript)) {
		t.Fatalf("grok cursor after backfill: %+v ok=%v want offset %d", c, ok, len(transcript))
	}
	h.store.mu.Lock()
	keys := make([]string, 0, len(h.store.putLog))
	for k := range h.store.putLog {
		keys = append(keys, k)
	}
	h.store.mu.Unlock()
	if len(keys) != 1 || !strings.HasPrefix(keys[0], "grok/"+uuidGrokB) {
		t.Fatalf("store received PUTs for %v, want only the grok transcript identity", keys)
	}
}

// TestGrokAppendShipsTail proves per-file append semantics: new bytes ship
// from the cursor, not from zero.
func TestGrokAppendShipsTail(t *testing.T) {
	h := newHarness(t)
	transcript := fixture(t, "grok-chat-history.jsonl")
	cut := len(transcript) / 2
	p := h.writeGrok(grokCwdGroup, uuidGrokB, transcript[:cut])
	h.runOnce()

	if err := os.WriteFile(p, transcript, 0o644); err != nil {
		t.Fatal(err)
	}
	before := len(h.store.puts("grok", uuidGrokB, uuidGrokB))
	h.runOnce()
	h.assertMirror("grok", uuidGrokB, transcript)
	after := h.store.puts("grok", uuidGrokB, uuidGrokB)
	for _, off := range after[before:] {
		if off == 0 {
			t.Fatalf("append pass re-shipped from zero: offsets %v", after[before:])
		}
	}
}
