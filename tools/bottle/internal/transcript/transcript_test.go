package transcript

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testdata = "../../testdata"

// fixtureLines returns the raw lines of a fixture (1-based access via index+1).
func fixtureLines(t *testing.T, name string) [][]byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testdata, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
	return lines
}

func parseFixtureLine(t *testing.T, name string, lineNum int) Entry {
	t.Helper()
	lines := fixtureLines(t, name)
	if lineNum > len(lines) {
		t.Fatalf("%s has %d lines, wanted line %d", name, len(lines), lineNum)
	}
	e, err := ParseEntry(lines[lineNum-1], lineNum)
	if err != nil {
		t.Fatalf("%s line %d: %v", name, lineNum, err)
	}
	return e
}

func TestParseEntryClassification(t *testing.T) {
	cases := []struct {
		fixture string
		line    int
		class   Class
		typ     string
		subtype string
	}{
		// Tree nodes — including attachment (census finding) and system subtypes.
		{"plain.jsonl", 5, ClassTreeNode, "user", ""},
		{"plain.jsonl", 9, ClassTreeNode, "assistant", ""},
		{"plain.jsonl", 3, ClassTreeNode, "attachment", ""},
		{"plain.jsonl", 28, ClassTreeNode, "system", "model_refusal_fallback"},
		{"branched.jsonl", 37, ClassTreeNode, "system", "turn_duration"},
		{"branched.jsonl", 71, ClassTreeNode, "system", "away_summary"},
		{"compacted.jsonl", 172, ClassTreeNode, "system", "stop_hook_summary"},
		{"compacted.jsonl", 223, ClassTreeNode, "system", "compact_boundary"},
		// Stateful trailers.
		{"plain.jsonl", 8, ClassTrailer, "last-prompt", ""},
		{"plain.jsonl", 17, ClassTrailer, "mode", ""},
		{"branched.jsonl", 2, ClassTrailer, "permission-mode", ""},
		// Operational lines.
		{"plain.jsonl", 1, ClassOperational, "queue-operation", ""},
		{"branched.jsonl", 5, ClassOperational, "file-history-snapshot", ""},
		{"branched.jsonl", 10, ClassOperational, "ai-title", ""},
	}
	for _, c := range cases {
		e := parseFixtureLine(t, c.fixture, c.line)
		if e.Type != c.typ || e.Subtype != c.subtype {
			t.Errorf("%s:%d type/subtype = %q/%q, want %q/%q", c.fixture, c.line, e.Type, e.Subtype, c.typ, c.subtype)
		}
		if got := e.Class(); got != c.class {
			t.Errorf("%s:%d Class() = %v, want %v", c.fixture, c.line, got, c.class)
		}
		if e.Line != c.line {
			t.Errorf("%s:%d Line = %d", c.fixture, c.line, e.Line)
		}
	}
}

func TestParseEntryUnknownTypes(t *testing.T) {
	// Unknown type without a uuid: pass-through class.
	e, err := ParseEntry([]byte(`{"type":"future-thing","sessionId":"s"}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if e.Class() != ClassUnknown {
		t.Errorf("unknown type without uuid: Class() = %v, want ClassUnknown", e.Class())
	}
	// Unknown type WITH uuid/parentUuid: it participates in the tree.
	e, err = ParseEntry([]byte(`{"type":"future-node","uuid":"u1","parentUuid":"u0"}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if e.Class() != ClassTreeNode {
		t.Errorf("unknown type with uuid: Class() = %v, want ClassTreeNode", e.Class())
	}
	// summary entries (not in the fixture corpus, but a known CC type with a
	// leafUuid reference) are operational.
	e, err = ParseEntry([]byte(`{"type":"summary","summary":"t","leafUuid":"u1"}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if e.Class() != ClassOperational {
		t.Errorf("summary: Class() = %v, want ClassOperational", e.Class())
	}
	if e.LeafUUID != "u1" {
		t.Errorf("summary LeafUUID = %q, want u1", e.LeafUUID)
	}
}

func TestParseEntryFields(t *testing.T) {
	// compact_boundary: parentUuid null, logicalParentUuid set.
	e := parseFixtureLine(t, "multi-compact.jsonl", 36)
	if !e.IsCompactBoundary() {
		t.Fatal("line 36 should be a compact boundary")
	}
	if e.ParentUUID != "" {
		t.Errorf("boundary ParentUUID = %q, want empty", e.ParentUUID)
	}
	if e.LogicalParentUUID != "191bbbeb-9a29-4865-93c8-aaa7b4074c1a" {
		t.Errorf("boundary LogicalParentUUID = %q", e.LogicalParentUUID)
	}
	// last-prompt: leafUuid + sessionId.
	e = parseFixtureLine(t, "plain.jsonl", 16)
	if e.LeafUUID != "7620faa0-a179-4058-9173-0bc815fe91b1" {
		t.Errorf("last-prompt LeafUUID = %q", e.LeafUUID)
	}
	if e.SessionID != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("last-prompt SessionID = %q", e.SessionID)
	}
	// file-history-snapshot: keyed by messageId, has no sessionId.
	e = parseFixtureLine(t, "branched.jsonl", 5)
	if e.MessageID != "20a9d984-0bd3-4089-b541-65a816e58e6b" {
		t.Errorf("fhs MessageID = %q", e.MessageID)
	}
	if e.SessionID != "" {
		t.Errorf("fhs SessionID = %q, want empty", e.SessionID)
	}
	// isCompactSummary user entry.
	e = parseFixtureLine(t, "multi-compact.jsonl", 37)
	if !e.IsCompactSummary {
		t.Error("line 37 should have IsCompactSummary")
	}
	// assistant tool_use ids surface for the self-bottle trim (U6).
	e = parseFixtureLine(t, "dangling-tool-use.jsonl", 26)
	if len(e.ToolUseIDs) != 1 || e.ToolUseIDs[0] != "toolu_011nCJc2XFskie4wh7Jbcv9g" {
		t.Errorf("ToolUseIDs = %v", e.ToolUseIDs)
	}
	// user tool_result carrier ids surface too.
	e = parseFixtureLine(t, "plain.jsonl", 27)
	if len(e.ToolResultIDs) != 1 || e.ToolResultIDs[0] != "toolu_011nCJc2XFskie4wh7Jbcv9g" {
		t.Errorf("ToolResultIDs = %v", e.ToolResultIDs)
	}
}

func TestIsUserTurn(t *testing.T) {
	cases := []struct {
		fixture string
		line    int
		want    bool
		why     string
	}{
		{"plain.jsonl", 5, true, "plain human prompt"},
		{"plain.jsonl", 27, false, "tool_result carrier"},
		{"queued.jsonl", 23, true, "queued human message delivered as text blocks"},
		{"multi-compact.jsonl", 37, false, "isCompactSummary entry"},
		{"multi-compact.jsonl", 38, false, "isMeta entry"},
		{"multi-compact.jsonl", 39, false, "<command-name> echo (no isMeta flag!)"},
		{"multi-compact.jsonl", 40, false, "<local-command-stdout> echo (no isMeta flag!)"},
		{"multi-compact.jsonl", 51, true, "human prompt after compaction"},
		{"compacted.jsonl", 19, false, "isMeta caveat carrier"},
		{"plain.jsonl", 9, false, "assistant is never a user turn"},
		{"plain.jsonl", 8, false, "trailer is never a user turn"},
	}
	for _, c := range cases {
		e := parseFixtureLine(t, c.fixture, c.line)
		if got := e.IsUserTurn(); got != c.want {
			t.Errorf("%s:%d IsUserTurn() = %v, want %v (%s)", c.fixture, c.line, got, c.want, c.why)
		}
	}
	// Turn text is the human prompt.
	e := parseFixtureLine(t, "plain.jsonl", 5)
	if e.UserText() != "Remember the codeword ALPHA. Reply with just OK." {
		t.Errorf("UserText() = %q", e.UserText())
	}
}

func TestParseEntryMalformed(t *testing.T) {
	if _, err := ParseEntry([]byte(`{not json`), 7); err == nil {
		t.Fatal("expected error for malformed JSON")
	} else if !strings.Contains(err.Error(), "line 7") {
		t.Errorf("error should name the line number, got: %v", err)
	}
	// Non-object JSON values are malformed transcript lines.
	if _, err := ParseEntry([]byte(`[1,2,3]`), 3); err == nil {
		t.Fatal("expected error for non-object line")
	} else if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error should name the line number, got: %v", err)
	}
}
