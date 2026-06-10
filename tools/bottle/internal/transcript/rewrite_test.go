package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const newID = "deadbeef-dead-4eef-8eef-deadbeefdead"

func rewriteFixture(t *testing.T, name string) ([][]byte, [][]byte) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(testdata, name))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Rewrite(bytes.NewReader(src), &out, newID); err != nil {
		t.Fatalf("Rewrite(%s): %v", name, err)
	}
	split := func(b []byte) [][]byte {
		return bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n"))
	}
	return split(src), split(out.Bytes())
}

// TestRewriteTouchesOnlySessionID is the plan's happy-path contract: every
// line's sessionId gets the fresh uuid and nothing else changes.
func TestRewriteTouchesOnlySessionID(t *testing.T) {
	for _, name := range []string{"plain.jsonl", "branched.jsonl", "compacted.jsonl",
		"multi-compact.jsonl", "queued.jsonl", "dangling-tool-use.jsonl"} {
		srcLines, outLines := rewriteFixture(t, name)
		if len(srcLines) != len(outLines) {
			t.Fatalf("%s: line count changed %d -> %d", name, len(srcLines), len(outLines))
		}
		for i := range srcLines {
			var src, out map[string]any
			if err := json.Unmarshal(srcLines[i], &src); err != nil {
				t.Fatalf("%s src line %d: %v", name, i+1, err)
			}
			if err := json.Unmarshal(outLines[i], &out); err != nil {
				t.Fatalf("%s out line %d does not parse: %v", name, i+1, err)
			}
			if _, had := src["sessionId"]; had {
				if out["sessionId"] != newID {
					t.Errorf("%s line %d: sessionId = %v, want %s", name, i+1, out["sessionId"], newID)
				}
				src["sessionId"] = newID
			} else if !bytes.Equal(srcLines[i], outLines[i]) {
				// file-history-snapshot has no sessionId — byte-identical.
				t.Errorf("%s line %d: line without sessionId was modified", name, i+1)
			}
			if !reflect.DeepEqual(src, out) {
				t.Errorf("%s line %d: fields beyond sessionId changed", name, i+1)
			}
		}
	}
}

// TestRewritePreservesBytes pins the byte-level guarantee: only the sessionId
// value's bytes change — key order, spacing, escaping all survive verbatim.
func TestRewritePreservesBytes(t *testing.T) {
	srcLines, outLines := rewriteFixture(t, "plain.jsonl")
	oldID := "00000000-0000-4000-8000-000000000001"
	for i := range srcLines {
		want := strings.Replace(string(srcLines[i]), `"`+oldID+`"`, `"`+newID+`"`, 1)
		if string(outLines[i]) != want {
			t.Errorf("line %d: bytes differ beyond the sessionId value\n got: %s\nwant: %s",
				i+1, outLines[i], want)
		}
	}
}

// A top-level sessionId must be replaced even when the same key appears
// nested inside message content (transcripts about transcripts).
func TestRewriteIgnoresNestedSessionID(t *testing.T) {
	line := `{"type":"user","message":{"content":"see {\"sessionId\":\"x\"} here","sessionId":"inner"},"sessionId":"old-id","uuid":"u1"}` + "\n"
	var out bytes.Buffer
	if err := Rewrite(strings.NewReader(line), &out, newID); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, `"sessionId":"`+newID+`"`) {
		t.Errorf("top-level sessionId not rewritten: %s", got)
	}
	if !strings.Contains(got, `"sessionId":"inner"`) {
		t.Errorf("nested sessionId must survive verbatim: %s", got)
	}
	if !strings.Contains(got, `{\"sessionId\":\"x\"}`) {
		t.Errorf("sessionId text inside a string must survive verbatim: %s", got)
	}
}

func TestRewriteMalformedLine(t *testing.T) {
	var out bytes.Buffer
	err := Rewrite(strings.NewReader("{\"type\":\"mode\",\"sessionId\":\"a\"}\n{broken\n"), &out, newID)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name line 2, got: %v", err)
	}
}

func TestNewSessionID(t *testing.T) {
	v4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]bool)
	for i := 0; i < 64; i++ {
		id, err := NewSessionID()
		if err != nil {
			t.Fatal(err)
		}
		if !v4.MatchString(id) {
			t.Fatalf("NewSessionID() = %q, not a v4 uuid", id)
		}
		if seen[id] {
			t.Fatalf("NewSessionID() repeated %q", id)
		}
		seen[id] = true
	}
}
