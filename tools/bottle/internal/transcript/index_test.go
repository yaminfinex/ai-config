package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func indexFixture(t *testing.T, name string) *Info {
	t.Helper()
	info, err := IndexFile(filepath.Join(testdata, name))
	if err != nil {
		t.Fatalf("IndexFile(%s): %v", name, err)
	}
	return info
}

func TestIndexAllFixturesParseClean(t *testing.T) {
	for name, lines := range map[string]int{
		"plain.jsonl":             31,
		"branched.jsonl":          71,
		"compacted.jsonl":         242,
		"multi-compact.jsonl":     81,
		"queued.jsonl":            48,
		"dangling-tool-use.jsonl": 26,
	} {
		info := indexFixture(t, name)
		if len(info.Entries) != lines {
			t.Errorf("%s: %d entries, want %d", name, len(info.Entries), lines)
		}
	}
}

// turnExpect pins the oracle-derived turn enumeration for a fixture: the
// human turns on the live branch, in order, with the assistant entry that
// completed each turn (the truncation cut leaf).
type turnExpect struct {
	user, respLeaf string
}

var fixtureTurns = map[string][]turnExpect{
	"plain.jsonl": {
		{"2bb3303f-6427-46ef-974a-5db518950c01", "754a8279-8357-460c-b2c8-2069d7a40039"},
		{"4ba0b8ea-b907-43e3-a5a8-217fd2268d88", "7620faa0-a179-4058-9173-0bc815fe91b1"},
		{"24068d39-cfc1-40de-8b6e-3a0e4464559a", "191bbbeb-9a29-4865-93c8-aaa7b4074c1a"},
	},
	"branched.jsonl": {
		{"20a9d984-0bd3-4089-b541-65a816e58e6b", "03b4c82a-0868-445d-8457-52927f0db91b"},
		{"1a4b1b79-9328-4f99-81e9-2aa7c7e9caac", "ae0cf1bf-055d-4e74-a8b6-204106bc17e4"},
		{"c06a1ce4-7092-47f8-bfbd-5130bf205d90", "d7835479-b2ed-4fe4-afc6-ec8eeddd62ec"},
	},
	"compacted.jsonl": {
		{"697a3275-2aeb-4792-9adf-3880ef580dac", "bcf72404-74b3-4316-bbe3-d7508bca5d52"},
		{"ba203a6f-6687-42af-83ce-8678596e0863", "c22b6fa7-7d57-4d68-b708-270d7372031d"},
		{"3d5aedb0-d24d-49ac-acfe-12c912acf4f5", "5d86ea74-fffe-4d78-9e39-1663d4aca268"},
	},
	"multi-compact.jsonl": {
		{"2bb3303f-6427-46ef-974a-5db518950c01", "754a8279-8357-460c-b2c8-2069d7a40039"},
		{"4ba0b8ea-b907-43e3-a5a8-217fd2268d88", "7620faa0-a179-4058-9173-0bc815fe91b1"},
		{"24068d39-cfc1-40de-8b6e-3a0e4464559a", "191bbbeb-9a29-4865-93c8-aaa7b4074c1a"},
		{"c760ba6b-b4ea-4b4c-99a0-ec36c2303963", "4828bde2-f133-4d26-9298-78acd852824a"},
		{"197ae843-a81b-4cea-86ca-88c650ec825b", "854d7fee-0f6d-468a-b27b-f35f1f4827c1"},
	},
	"queued.jsonl": {
		{"9298d7a2-c0c9-4f16-838f-b47e40b8cdf0", "7d9bb364-296e-4c8e-b6da-8deb168a0890"},
		// A queued message delivered immediately before the next prompt has
		// no completing assistant response of its own.
		{"f4d85d34-a30b-4882-b949-2eec7567d001", ""},
		{"e6c8002e-bed6-4d6d-84c2-e114662841d8", "7deac210-cd4a-4d78-83e6-f08e2b63093f"},
	},
	"dangling-tool-use.jsonl": {
		{"2bb3303f-6427-46ef-974a-5db518950c01", "754a8279-8357-460c-b2c8-2069d7a40039"},
		{"4ba0b8ea-b907-43e3-a5a8-217fd2268d88", "7620faa0-a179-4058-9173-0bc815fe91b1"},
		{"24068d39-cfc1-40de-8b6e-3a0e4464559a", "182829b2-7233-4897-85f8-aef84c2bbb59"},
	},
}

func TestTurnsAllFixtures(t *testing.T) {
	for name, want := range fixtureTurns {
		turns := indexFixture(t, name).Turns()
		if len(turns) != len(want) {
			t.Errorf("%s: %d turns, want %d", name, len(turns), len(want))
			continue
		}
		for i, w := range want {
			got := turns[i]
			if got.N != i+1 {
				t.Errorf("%s turn %d: N = %d", name, i+1, got.N)
			}
			if got.UUID != w.user {
				t.Errorf("%s turn %d: UUID = %s, want %s", name, i+1, got.UUID, w.user)
			}
			if got.ResponseLeafUUID != w.respLeaf {
				t.Errorf("%s turn %d: ResponseLeafUUID = %s, want %s", name, i+1, got.ResponseLeafUUID, w.respLeaf)
			}
		}
	}
}

func TestTurnsCarryTextAndTimestamp(t *testing.T) {
	turns := indexFixture(t, "multi-compact.jsonl").Turns()
	if turns[3].Text != "Remember the codeword DELTA. Reply with just OK." {
		t.Errorf("turn 4 Text = %q", turns[3].Text)
	}
	if turns[0].Timestamp == "" {
		t.Error("turn 1 should carry the user entry's timestamp")
	}
	if turns[0].Line != 5 {
		t.Errorf("turn 1 Line = %d, want 5", turns[0].Line)
	}
}

func TestTurnsDanglingToolUse(t *testing.T) {
	turns := indexFixture(t, "dangling-tool-use.jsonl").Turns()
	for i, want := range []bool{false, false, true} {
		if turns[i].DanglingToolUse != want {
			t.Errorf("dangling turn %d: DanglingToolUse = %v, want %v", i+1, turns[i].DanglingToolUse, want)
		}
	}
	// The same prompt sequence with the tool_result present is not dangling.
	turns = indexFixture(t, "plain.jsonl").Turns()
	for i := range turns {
		if turns[i].DanglingToolUse {
			t.Errorf("plain turn %d unexpectedly flagged DanglingToolUse", i+1)
		}
	}
}

// TestTurnsListOnlyLiveBranch synthesizes an in-session rewind on top of
// plain.jsonl: a human prompt re-branched from the BRAVO assistant, making
// the CHARLIE turn a dead branch. Enumeration must list the live branch only.
func TestTurnsListOnlyLiveBranch(t *testing.T) {
	lines := fixtureLines(t, "plain.jsonl")
	userTmpl := mutateLine(t, lines[13], map[string]any{ // line 14: BRAVO user
		"uuid":       "aaaaaaaa-0000-4000-8000-000000000001",
		"parentUuid": "7620faa0-a179-4058-9173-0bc815fe91b1",
	})
	userTmpl = mutateMessageContent(t, userTmpl, "Actually, let's talk about pelicans instead.")
	asstTmpl := mutateLine(t, lines[14], map[string]any{ // line 15: assistant
		"uuid":       "aaaaaaaa-0000-4000-8000-000000000002",
		"parentUuid": "aaaaaaaa-0000-4000-8000-000000000001",
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "rewound.jsonl")
	var b strings.Builder
	for _, l := range lines {
		b.Write(l)
		b.WriteByte('\n')
	}
	b.WriteString(userTmpl + "\n" + asstTmpl + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := IndexFile(path)
	if err != nil {
		t.Fatal(err)
	}
	turns := info.Turns()
	if len(turns) != 3 {
		t.Fatalf("expected 3 live-branch turns, got %d", len(turns))
	}
	if turns[2].UUID != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Errorf("turn 3 = %s, want the rewound prompt", turns[2].UUID)
	}
	for _, turn := range turns {
		if turn.UUID == "24068d39-cfc1-40de-8b6e-3a0e4464559a" {
			t.Error("dead-branch CHARLIE turn must not be enumerated")
		}
	}
}

// mutateLine parses a fixture line, overrides top-level fields, re-marshals.
// Test-only helper: field order is allowed to change here.
func mutateLine(t *testing.T, line []byte, set map[string]any) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range set {
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func mutateMessageContent(t *testing.T, line string, text string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	msg, ok := m["message"].(map[string]any)
	if !ok {
		t.Fatal("line has no message object")
	}
	msg["content"] = text
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestIndexMalformedLineNamesLineNumber(t *testing.T) {
	lines := fixtureLines(t, "plain.jsonl")
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	corrupted := make([][]byte, len(lines))
	copy(corrupted, lines)
	corrupted[4] = []byte(`{"type":"user","uuid":`) // line 5 truncated mid-object
	if err := os.WriteFile(path, append(joinLines(corrupted), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := IndexFile(path)
	if err == nil {
		t.Fatal("expected error for malformed line")
	}
	if !strings.Contains(err.Error(), "line 5") {
		t.Errorf("error should name line 5, got: %v", err)
	}
}

func joinLines(lines [][]byte) []byte {
	out := make([]byte, 0, 1<<16)
	for i, l := range lines {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, l...)
	}
	return out
}

func TestNoTurns(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := IndexFile(empty)
	if err != nil {
		t.Fatalf("empty file should index cleanly: %v", err)
	}
	if len(info.Turns()) != 0 {
		t.Error("empty file should have no turns")
	}

	// Header-only: trailers and operational lines, no tree nodes.
	lines := fixtureLines(t, "queued.jsonl")
	headerOnly := filepath.Join(dir, "header.jsonl")
	if err := os.WriteFile(headerOnly, append(joinLines(lines[0:3]), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err = IndexFile(headerOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Turns()) != 0 {
		t.Error("header-only file should have no turns")
	}
}

func TestInfoHelpers(t *testing.T) {
	info := indexFixture(t, "multi-compact.jsonl")
	if got := info.CompactBoundaries(); got != 2 {
		t.Errorf("CompactBoundaries() = %d, want 2", got)
	}
	if got := info.LastLeaf(); got != "854d7fee-0f6d-468a-b27b-f35f1f4827c1" {
		t.Errorf("LastLeaf() = %s", got)
	}
	if got := indexFixture(t, "plain.jsonl").CompactBoundaries(); got != 0 {
		t.Errorf("plain CompactBoundaries() = %d, want 0", got)
	}
	// branched ends in a system/away_summary entry — still the resume leaf.
	if got := indexFixture(t, "branched.jsonl").LastLeaf(); got != "c8227677-a67b-47a6-88c5-b918baa1b1fe" {
		t.Errorf("branched LastLeaf() = %q", got)
	}
}

func TestEffectivePermissionMode(t *testing.T) {
	// queued.jsonl carries permission-mode trailers and user entries all stamped
	// bypassPermissions; multi-compact runs entirely in default.
	if got := indexFixture(t, "queued.jsonl").EffectivePermissionMode(); got != "bypassPermissions" {
		t.Errorf("queued EffectivePermissionMode() = %q, want bypassPermissions", got)
	}
	if got := indexFixture(t, "multi-compact.jsonl").EffectivePermissionMode(); got != "default" {
		t.Errorf("multi-compact EffectivePermissionMode() = %q, want default", got)
	}
	// A transcript that records no permissionMode at all yields "".
	info := indexFixture(t, "plain.jsonl")
	for i := range info.Entries {
		info.Entries[i].PermissionMode = ""
	}
	if got := info.EffectivePermissionMode(); got != "" {
		t.Errorf("stripped EffectivePermissionMode() = %q, want empty", got)
	}
}
