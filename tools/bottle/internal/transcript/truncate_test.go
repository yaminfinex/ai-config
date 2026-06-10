package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func truncateFixtureAtTurn(t *testing.T, name string, turn int) []byte {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "out.jsonl")
	if err := TruncateFileAtTurn(filepath.Join(testdata, name), dst, turn); err != nil {
		t.Fatalf("TruncateFileAtTurn(%s, %d): %v", name, turn, err)
	}
	out, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func truncateFixtureAtLeaf(t *testing.T, name, leaf string) []byte {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "out.jsonl")
	if err := TruncateFileAtLeaf(filepath.Join(testdata, name), dst, leaf); err != nil {
		t.Fatalf("TruncateFileAtLeaf(%s, %s): %v", name, leaf, err)
	}
	out, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func indexBytes(t *testing.T, b []byte) *Info {
	t.Helper()
	info, err := Index(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("truncate output does not re-parse: %v", err)
	}
	return info
}

func mustLint(t *testing.T, b []byte) {
	t.Helper()
	if err := Lint(bytes.NewReader(b)); err != nil {
		t.Fatalf("truncate output fails dangling-uuid lint: %v", err)
	}
}

func hasTreeUUID(info *Info, uuid string) bool {
	for i := range info.Entries {
		if info.Entries[i].UUID == uuid {
			return true
		}
	}
	return false
}

func finalTrailer(t *testing.T, b []byte) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n"))
	var m map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &m); err != nil {
		t.Fatalf("final line does not parse: %v", err)
	}
	return m
}

// TestTruncatePlainAtTurn2MatchesSmokeCut pins the marquee contract byte for
// byte: truncating plain.jsonl at turn 2 reproduces exactly the 16-line cut
// that U1's smoke script resumed against a live harness (tree through the
// BRAVO assistant reply, plus a valid final last-prompt trailer).
func TestTruncatePlainAtTurn2MatchesSmokeCut(t *testing.T) {
	out := truncateFixtureAtTurn(t, "plain.jsonl", 2)
	src, err := os.ReadFile(filepath.Join(testdata, "plain.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	srcLines := bytes.SplitAfter(src, []byte("\n"))
	want := bytes.Join(srcLines[:16], nil)
	if !bytes.Equal(out, want) {
		t.Errorf("output differs from the smoke-verified 16-line cut\n got (%d bytes):\n%s\nwant (%d bytes):\n%s",
			len(out), out, len(want), want)
	}
}

func TestTruncateKeepsThroughCompletingResponse(t *testing.T) {
	out := truncateFixtureAtTurn(t, "plain.jsonl", 1)
	info := indexBytes(t, out)
	mustLint(t, out)

	if got := info.LastLeaf(); got != "754a8279-8357-460c-b2c8-2069d7a40039" {
		t.Errorf("LastLeaf = %s, want the ALPHA turn's completing assistant", got)
	}
	if hasTreeUUID(info, "4ba0b8ea-b907-43e3-a5a8-217fd2268d88") {
		t.Error("turn 2's user entry must be cut")
	}
	trailer := finalTrailer(t, out)
	if trailer["type"] != "last-prompt" {
		t.Fatalf("final line type = %v, want last-prompt", trailer["type"])
	}
	if trailer["leafUuid"] != "754a8279-8357-460c-b2c8-2069d7a40039" {
		t.Errorf("final trailer leafUuid = %v, want the new tail", trailer["leafUuid"])
	}
	if trailer["lastPrompt"] != "Remember the codeword ALPHA. Reply with just OK." {
		t.Errorf("final trailer lastPrompt = %v, want the cut turn's text", trailer["lastPrompt"])
	}
}

// Truncating between compact boundaries (the README's smoke scenario: cut
// after DELTA's completing reply) keeps boundary #1 + its summary and drops
// boundary #2 and everything after.
func TestTruncateMultiCompactBetweenBoundaries(t *testing.T) {
	out := truncateFixtureAtTurn(t, "multi-compact.jsonl", 4)
	info := indexBytes(t, out)
	mustLint(t, out)

	if got := info.LastLeaf(); got != "4828bde2-f133-4d26-9298-78acd852824a" {
		t.Errorf("LastLeaf = %s, want DELTA's completing assistant", got)
	}
	if got := info.CompactBoundaries(); got != 1 {
		t.Errorf("CompactBoundaries = %d, want 1 (boundary #2 cut)", got)
	}
	if !hasTreeUUID(info, "89bf59a7-f9df-4269-989a-7a207806c0cd") {
		t.Error("boundary #1 must survive")
	}
	if !hasTreeUUID(info, "35eaa556-55f4-4f2c-8d02-fbdd9b5d8ca5") {
		t.Error("boundary #1's isCompactSummary entry must survive")
	}
	if hasTreeUUID(info, "94e971df-947e-482c-a3a8-5404af7a392c") {
		t.Error("boundary #2 must be cut")
	}
	if hasTreeUUID(info, "197ae843-a81b-4cea-86ca-88c650ec825b") {
		t.Error("the ECHO turn past the cut must be gone")
	}
}

func TestTruncateBeforeAnyBoundary(t *testing.T) {
	out := truncateFixtureAtTurn(t, "multi-compact.jsonl", 3)
	info := indexBytes(t, out)
	mustLint(t, out)
	if got := info.CompactBoundaries(); got != 0 {
		t.Errorf("CompactBoundaries = %d, want 0 (cut precedes both)", got)
	}
	if got := info.LastLeaf(); got != "191bbbeb-9a29-4865-93c8-aaa7b4074c1a" {
		t.Errorf("LastLeaf = %s", got)
	}
}

// Cutting at the leaf the smoke script pinned (the attachment closing
// boundary #1's summary block) reproduces its 44-line shape: 43 source lines
// verbatim plus a repaired trailer.
func TestTruncateAtLeafSummaryBlock(t *testing.T) {
	out := truncateFixtureAtLeaf(t, "multi-compact.jsonl", "638b66ad-b54d-40bc-a257-b1b171fde992")
	info := indexBytes(t, out)
	mustLint(t, out)

	src, err := os.ReadFile(filepath.Join(testdata, "multi-compact.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	outLines := bytes.SplitAfter(out, []byte("\n"))
	srcLines := bytes.SplitAfter(src, []byte("\n"))
	if !bytes.Equal(bytes.Join(outLines[:43], nil), bytes.Join(srcLines[:43], nil)) {
		t.Error("first 43 lines must match the source verbatim")
	}
	if n := len(bytes.Split(bytes.TrimSuffix(out, []byte("\n")), []byte("\n"))); n != 44 {
		t.Errorf("output has %d lines, want 44 (smoke-pinned shape)", n)
	}
	if got := info.LastLeaf(); got != "638b66ad-b54d-40bc-a257-b1b171fde992" {
		t.Errorf("LastLeaf = %s", got)
	}
	trailer := finalTrailer(t, out)
	if trailer["leafUuid"] != "638b66ad-b54d-40bc-a257-b1b171fde992" {
		t.Errorf("trailer leafUuid = %v", trailer["leafUuid"])
	}
}

// A cut aimed exactly at a compact_boundary entry must keep boundary +
// summary atomic: the cut extends through the isCompactSummary entry.
func TestTruncateAtBoundaryKeepsSummaryAtomic(t *testing.T) {
	out := truncateFixtureAtLeaf(t, "multi-compact.jsonl", "89bf59a7-f9df-4269-989a-7a207806c0cd")
	info := indexBytes(t, out)
	mustLint(t, out)
	if !hasTreeUUID(info, "89bf59a7-f9df-4269-989a-7a207806c0cd") {
		t.Error("boundary must be present")
	}
	if got := info.LastLeaf(); got != "35eaa556-55f4-4f2c-8d02-fbdd9b5d8ca5" {
		t.Errorf("LastLeaf = %s, want the isCompactSummary entry (atomic block)", got)
	}
}

func TestTruncateQueuedDropsQueueOps(t *testing.T) {
	out := truncateFixtureAtTurn(t, "queued.jsonl", 1)
	info := indexBytes(t, out)
	mustLint(t, out)

	for i := range info.Entries {
		if info.Entries[i].Type == "queue-operation" {
			t.Error("queue-operation lines for cut messages must be dropped")
		}
	}
	if hasTreeUUID(info, "f4d85d34-a30b-4882-b949-2eec7567d001") {
		t.Error("the queued message past the cut must be gone")
	}
	// file-history-snapshot bookkeeping follows its message across the cut.
	var mids []string
	for i := range info.Entries {
		if info.Entries[i].Type == "file-history-snapshot" {
			mids = append(mids, info.Entries[i].MessageID)
		}
	}
	if len(mids) != 1 || mids[0] != "9298d7a2-c0c9-4f16-838f-b47e40b8cdf0" {
		t.Errorf("file-history-snapshots = %v, want only the kept turn's", mids)
	}
}

func TestTruncateTurnWithoutResponse(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out.jsonl")
	err := TruncateFileAtTurn(filepath.Join(testdata, "queued.jsonl"), dst, 2)
	if err == nil {
		t.Fatal("turn 2 has no completing assistant response; truncation must refuse")
	}
	if !strings.Contains(err.Error(), "turn 2") {
		t.Errorf("error should name the turn, got: %v", err)
	}
	if _, statErr := os.Stat(dst); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("no output may be written on refusal")
	}
}

// A prefix cut is a temporal cut: rewind branches that existed before the cut
// stay (their parents resolve), ones recorded after it go.
func TestTruncateBranchedDeadBranches(t *testing.T) {
	// Last turn: the dead-branch tool_result user (line 66) predates the cut
	// leaf (line 69) and is kept — a prefix is what the live harness was
	// verified to resume.
	out := truncateFixtureAtTurn(t, "branched.jsonl", 3)
	info := indexBytes(t, out)
	mustLint(t, out)
	if !hasTreeUUID(info, "e00c2008-5eab-4dce-b2d5-ef02b9634ac7") {
		t.Error("dead branch recorded before the cut must be kept (temporal prefix)")
	}

	// Turn 2: everything after its completing assistant goes, dead branch
	// included.
	out2 := truncateFixtureAtTurn(t, "branched.jsonl", 2)
	info2 := indexBytes(t, out2)
	mustLint(t, out2)
	if hasTreeUUID(info2, "e00c2008-5eab-4dce-b2d5-ef02b9634ac7") {
		t.Error("dead branch recorded after the cut must be gone")
	}
	if got := info2.LastLeaf(); got != "ae0cf1bf-055d-4e74-a8b6-204106bc17e4" {
		t.Errorf("LastLeaf = %s", got)
	}
	for i := range info2.Entries {
		if info2.Entries[i].Subtype == "away_summary" {
			t.Error("away_summary past the cut must be gone")
		}
	}
}

func TestTruncateDanglingToolUseAtLastTurn(t *testing.T) {
	out := truncateFixtureAtTurn(t, "dangling-tool-use.jsonl", 3)
	info := indexBytes(t, out)
	mustLint(t, out)
	if got := info.LastLeaf(); got != "182829b2-7233-4897-85f8-aef84c2bbb59" {
		t.Errorf("LastLeaf = %s, want the dangling tool_use assistant (U1: resume tolerates it)", got)
	}
	trailer := finalTrailer(t, out)
	if trailer["leafUuid"] != "182829b2-7233-4897-85f8-aef84c2bbb59" {
		t.Errorf("trailer leafUuid = %v", trailer["leafUuid"])
	}
}

// In-prefix operational/trailer lines whose references point past the cut are
// repaired away (the forward-pointing summary/ai-title case from the plan).
func TestTruncateDropsForwardDanglingRefsInsidePrefix(t *testing.T) {
	lines := fixtureLines(t, "plain.jsonl")
	withSummary := make([][]byte, 0, len(lines)+1)
	// A summary line up front referencing the CHARLIE-turn assistant that the
	// cut will remove.
	withSummary = append(withSummary,
		[]byte(`{"type":"summary","summary":"sanitized","leafUuid":"191bbbeb-9a29-4865-93c8-aaa7b4074c1a"}`))
	withSummary = append(withSummary, lines...)

	dir := t.TempDir()
	src := filepath.Join(dir, "with-summary.jsonl")
	if err := os.WriteFile(src, append(joinLines(withSummary), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.jsonl")
	if err := TruncateFileAtTurn(src, dst, 2); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	mustLint(t, out)
	info := indexBytes(t, out)
	for i := range info.Entries {
		if info.Entries[i].Type == "summary" {
			t.Error("summary referencing a cut uuid must be dropped")
		}
	}
	// Same source truncated at turn 3 keeps it (ref resolves).
	dst3 := filepath.Join(dir, "out3.jsonl")
	if err := TruncateFileAtTurn(src, dst3, 3); err != nil {
		t.Fatal(err)
	}
	out3, err := os.ReadFile(dst3)
	if err != nil {
		t.Fatal(err)
	}
	mustLint(t, out3)
	found := false
	for _, e := range indexBytes(t, out3).Entries {
		if e.Type == "summary" {
			found = true
		}
	}
	if !found {
		t.Error("summary with a resolving ref must be kept")
	}
}

// Unknown entry types: pass through untouched on copy, dropped after the cut
// (the forward-compat default from the plan).
func TestTruncateUnknownTypesPassThrough(t *testing.T) {
	lines := fixtureLines(t, "plain.jsonl")
	unknown := []byte(`{"type":"future-thing","payload":{"x":1},"sessionId":"00000000-0000-4000-8000-000000000001"}`)
	withUnknown := make([][]byte, 0, len(lines)+2)
	withUnknown = append(withUnknown, unknown)  // before the cut
	withUnknown = append(withUnknown, lines...) // lines 2..32
	withUnknown = append(withUnknown, unknown)  // after the cut

	dir := t.TempDir()
	src := filepath.Join(dir, "with-unknown.jsonl")
	if err := os.WriteFile(src, append(joinLines(withUnknown), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.jsonl")
	if err := TruncateFileAtTurn(src, dst, 2); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	mustLint(t, out)
	count := 0
	for _, e := range indexBytes(t, out).Entries {
		if e.Type == "future-thing" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("unknown-type lines in output = %d, want 1 (kept before cut, dropped after)", count)
	}
}

func TestTruncateErrorPaths(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(testdata, "plain.jsonl")

	// Turn out of range.
	if err := TruncateFileAtTurn(src, filepath.Join(dir, "a.jsonl"), 9); err == nil {
		t.Error("turn out of range must error")
	}
	if err := TruncateFileAtTurn(src, filepath.Join(dir, "b.jsonl"), 0); err == nil {
		t.Error("turn 0 must error")
	}
	// Unknown leaf.
	if err := TruncateFileAtLeaf(src, filepath.Join(dir, "c.jsonl"), "ffffffff-0000-4000-8000-000000000000"); err == nil {
		t.Error("unknown leaf uuid must error")
	}
	// A trailer has no uuid and cannot be a cut leaf.
	if err := TruncateFileAtLeaf(src, filepath.Join(dir, "d.jsonl"), ""); err == nil {
		t.Error("empty leaf uuid must error")
	}

	// Empty + header-only sources: ErrNoTurns.
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TruncateFileAtTurn(empty, filepath.Join(dir, "e.jsonl"), 1); !errors.Is(err, ErrNoTurns) {
		t.Errorf("empty file: err = %v, want ErrNoTurns", err)
	}
	headerOnly := filepath.Join(dir, "header.jsonl")
	if err := os.WriteFile(headerOnly, append(joinLines(fixtureLines(t, "queued.jsonl")[0:3]), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TruncateFileAtTurn(headerOnly, filepath.Join(dir, "f.jsonl"), 1); !errors.Is(err, ErrNoTurns) {
		t.Errorf("header-only file: err = %v, want ErrNoTurns", err)
	}
}

func TestTruncateMalformedNoPartialOutput(t *testing.T) {
	lines := fixtureLines(t, "plain.jsonl")
	corrupted := append([][]byte(nil), lines...)
	corrupted[4] = []byte(`{"type":"user","uuid":`)
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(src, append(joinLines(corrupted), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.jsonl")
	err := TruncateFileAtTurn(src, dst, 1)
	if err == nil {
		t.Fatal("malformed source must error")
	}
	if !strings.Contains(err.Error(), "line 5") {
		t.Errorf("error should name line 5, got: %v", err)
	}
	if _, statErr := os.Stat(dst); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("no partial output may be written")
	}
	// No stray temp files either.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory should contain only the source, got %v", names)
	}
}

// TestRoundTripAllFixturesAllTurns is the unit's verification gate: for every
// fixture and every truncatable turn, parse → truncate → write → re-parse
// with zero dangling uuid references, then compose with a session-id rewrite
// (the decant pipeline) and re-lint.
func TestRoundTripAllFixturesAllTurns(t *testing.T) {
	for name, want := range fixtureTurns {
		info := indexFixture(t, name)
		turns := info.Turns()
		for i, turn := range turns {
			if turn.ResponseLeafUUID == "" {
				continue
			}
			out := truncateFixtureAtTurn(t, name, turn.N)
			outInfo := indexBytes(t, out)
			mustLint(t, out)

			if got := outInfo.LastLeaf(); got != want[i].respLeaf {
				t.Errorf("%s turn %d: LastLeaf = %s, want %s", name, turn.N, got, want[i].respLeaf)
			}
			trailer := finalTrailer(t, out)
			if trailer["type"] != "last-prompt" {
				t.Errorf("%s turn %d: final line is %v, want repaired last-prompt", name, turn.N, trailer["type"])
			} else if trailer["leafUuid"] != want[i].respLeaf {
				t.Errorf("%s turn %d: trailer leafUuid = %v, want %s", name, turn.N, trailer["leafUuid"], want[i].respLeaf)
			}

			// Truncated turns re-enumerate as the prefix of the original.
			gotTurns := outInfo.Turns()
			if len(gotTurns) != turn.N {
				t.Errorf("%s turn %d: output enumerates %d turns", name, turn.N, len(gotTurns))
			}

			// Decant composition: rewrite the session id, re-lint, topology intact.
			var rewritten bytes.Buffer
			if err := Rewrite(bytes.NewReader(out), &rewritten, newID); err != nil {
				t.Fatalf("%s turn %d: rewrite after truncate: %v", name, turn.N, err)
			}
			mustLint(t, rewritten.Bytes())
			rwInfo := indexBytes(t, rewritten.Bytes())
			if len(rwInfo.Entries) != len(outInfo.Entries) {
				t.Errorf("%s turn %d: rewrite changed entry count", name, turn.N)
			}
			for j := range rwInfo.Entries {
				if rwInfo.Entries[j].UUID != outInfo.Entries[j].UUID ||
					rwInfo.Entries[j].ParentUUID != outInfo.Entries[j].ParentUUID {
					t.Errorf("%s turn %d: rewrite disturbed the parentUuid topology", name, turn.N)
					break
				}
				if rwInfo.Entries[j].SessionID != "" && rwInfo.Entries[j].SessionID != newID {
					t.Errorf("%s turn %d line %d: sessionId not rewritten", name, turn.N, rwInfo.Entries[j].Line)
					break
				}
			}
		}
	}
}
