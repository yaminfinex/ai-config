package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The named churn cases of the fixture corpus (provenance in
// fixtures/README.md). Each must be present and carry the property it was
// captured for; these tests check line-JSONL shape and the churn property,
// never a harness schema.
const (
	fixNormal       = "claude-normal.jsonl"
	fixResumeOrig   = "claude-resume-original.jsonl"
	fixResumeNew    = "claude-resume-new-file.jsonl"
	fixPartial      = "claude-trailing-partial.jsonl"
	fixInterleaved  = "claude-interleaved-writers-standin.jsonl"
	fixCodexRollout = "codex-rollout-meta.jsonl"
	fixGrokChat     = "grok-chat-history.jsonl"
)

func fixturePath(name string) string {
	return filepath.Join("fixtures", name)
}

// readLines splits a fixture into lines without requiring them to be JSON.
func readLines(t *testing.T, name string) [][]byte {
	t.Helper()
	raw, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	if len(raw) == 0 {
		t.Fatalf("fixture %s is empty", name)
	}
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1] // trailing newline
	}
	return lines
}

type entry struct {
	UUID        string `json:"uuid"`
	ParentUUID  string `json:"parentUuid"`
	SessionID   string `json:"sessionId"`
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
}

func parseEntries(t *testing.T, name string) []entry {
	t.Helper()
	var out []entry
	for i, line := range readLines(t, name) {
		if !json.Valid(line) {
			t.Fatalf("fixture %s line %d: not valid JSON", name, i+1)
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("fixture %s line %d: %v", name, i+1, err)
		}
		out = append(out, e)
	}
	return out
}

func TestFixtureInventoryPresent(t *testing.T) {
	for _, name := range []string{
		fixNormal, fixResumeOrig, fixResumeNew,
		fixPartial, fixInterleaved, fixCodexRollout, fixGrokChat,
	} {
		if _, err := os.Stat(fixturePath(name)); err != nil {
			t.Errorf("named churn case missing: %v", err)
		}
	}
}

func TestCompleteFixturesAreLineJSONL(t *testing.T) {
	for _, name := range []string{fixNormal, fixResumeOrig, fixResumeNew, fixInterleaved, fixCodexRollout, fixGrokChat} {
		raw, err := os.ReadFile(fixturePath(name))
		if err != nil {
			t.Fatal(err)
		}
		if raw[len(raw)-1] != '\n' {
			t.Errorf("%s: complete fixture must end with a newline", name)
		}
		parseEntries(t, name) // fails the test on any invalid line
	}
}

func TestResumePairOverlapsButClaimsPerFileSessionIDs(t *testing.T) {
	orig := parseEntries(t, fixResumeOrig)
	resumed := parseEntries(t, fixResumeNew)

	seen := map[string]bool{}
	for _, e := range orig {
		if e.UUID != "" {
			seen[e.UUID] = true
		}
	}
	overlap := 0
	for _, e := range resumed {
		if seen[e.UUID] {
			overlap++
		}
	}
	if overlap == 0 {
		t.Error("resume pair shares no message uuids; the S2 dedup case is gone")
	}

	// The verified churn property: content sessionId follows the file claim,
	// so the two files must each be uniform and disagree with each other.
	sids := func(es []entry, name string) string {
		sid := ""
		for i, e := range es {
			if e.SessionID == "" {
				continue
			}
			if sid == "" {
				sid = e.SessionID
			} else if e.SessionID != sid {
				t.Errorf("%s line %d: second sessionId %q (expected uniform %q)", name, i+1, e.SessionID, sid)
			}
		}
		return sid
	}
	if a, b := sids(orig, fixResumeOrig), sids(resumed, fixResumeNew); a == "" || b == "" || a == b {
		t.Errorf("resume pair sessionIds must be per-file and distinct, got %q vs %q", a, b)
	}
}

// TestGrokFixtureCarriesNoUUIDsOrTimestamps pins the captured property the
// grok index semantics stand on: chat_history lines have no message uuid, no
// timestamp, and no session id field, and the fixture holds the full live
// entry-type spread.
func TestGrokFixtureCarriesNoUUIDsOrTimestamps(t *testing.T) {
	types := map[string]int{}
	for i, line := range readLines(t, fixGrokChat) {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			t.Fatalf("fixture %s line %d: %v", fixGrokChat, i+1, err)
		}
		for _, key := range []string{"uuid", "timestamp", "sessionId", "session_id", "ts"} {
			if _, ok := raw[key]; ok {
				t.Fatalf("fixture %s line %d carries %q; the grok no-uuid/no-timestamp property is gone — recut and revisit the index semantics", fixGrokChat, i+1, key)
			}
		}
		var typ string
		if err := json.Unmarshal(raw["type"], &typ); err != nil {
			t.Fatalf("fixture %s line %d: type field: %v", fixGrokChat, i+1, err)
		}
		types[typ]++
	}
	for _, typ := range []string{"system", "user", "assistant", "reasoning", "tool_result"} {
		if types[typ] == 0 {
			t.Errorf("fixture %s lost the %q entry type from the live spread", fixGrokChat, typ)
		}
	}
}

func TestTrailingPartialIsRealPrefixWithBrokenLastLine(t *testing.T) {
	partial, err := os.ReadFile(fixturePath(fixPartial))
	if err != nil {
		t.Fatal(err)
	}
	full, err := os.ReadFile(fixturePath(fixNormal))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(full, partial) {
		t.Fatal("trailing-partial fixture is not a byte prefix of its recorded source (see fixtures/README.md)")
	}
	if partial[len(partial)-1] == '\n' {
		t.Fatal("trailing-partial fixture ends on a line boundary; it must cut mid-line")
	}

	sc := bufio.NewScanner(bytes.NewReader(partial))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	var last []byte
	complete := 0
	for sc.Scan() {
		last = append(last[:0], sc.Bytes()...)
		complete++
	}
	if complete < 2 {
		t.Fatalf("want several complete lines before the partial tail, got %d", complete)
	}
	if json.Valid(last) {
		t.Error("last line of trailing-partial fixture parses as JSON; it must be a partial line")
	}
}

func TestInterleavedStandinHasForkedChains(t *testing.T) {
	children := map[string]int{}
	for _, e := range parseEntries(t, fixInterleaved) {
		if !e.IsSidechain && e.ParentUUID != "" {
			children[e.ParentUUID]++
		}
	}
	forks := 0
	for _, n := range children {
		if n > 1 {
			forks++
		}
	}
	if forks == 0 {
		t.Error("interleaved-writers stand-in has no forked parentUuid chains; the non-linear-order property is gone")
	}
}

func TestCodexRolloutStartsWithSessionMeta(t *testing.T) {
	lines := readLines(t, fixCodexRollout)
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(lines[0], &head); err != nil {
		t.Fatalf("codex rollout line 1: %v", err)
	}
	if head.Type != "session_meta" {
		t.Errorf("codex rollout line 1 type = %q, want session_meta", head.Type)
	}
}
