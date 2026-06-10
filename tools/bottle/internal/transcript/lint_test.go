package transcript

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lintBytes(t *testing.T, b []byte) error {
	t.Helper()
	return Lint(bytes.NewReader(b))
}

// Pristine fixtures must lint clean — every parentUuid, logicalParentUuid,
// last-prompt leafUuid and file-history-snapshot messageId resolves.
// (compactMetadata uuid lists are deliberately out of scope: real Claude Code
// files reference rewound-away uuids there even before any surgery.)
func TestLintFixturesClean(t *testing.T) {
	for _, name := range []string{"plain.jsonl", "branched.jsonl", "compacted.jsonl",
		"multi-compact.jsonl", "queued.jsonl", "dangling-tool-use.jsonl"} {
		raw, err := os.ReadFile(filepath.Join(testdata, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := lintBytes(t, raw); err != nil {
			t.Errorf("%s: pristine fixture fails lint: %v", name, err)
		}
	}
}

func TestLintCatchesDanglingRefs(t *testing.T) {
	lines := fixtureLines(t, "plain.jsonl")

	// Remove a tree node another entry's parentUuid points at.
	missingParent := make([][]byte, 0, len(lines))
	for i, l := range lines {
		if i == 8 { // line 9: assistant 5e5afadd, parent of line 10
			continue
		}
		missingParent = append(missingParent, l)
	}
	err := lintBytes(t, joinLines(missingParent))
	if err == nil {
		t.Fatal("lint must catch a missing parentUuid target")
	}
	if !strings.Contains(err.Error(), "5e5afadd") {
		t.Errorf("lint error should name the dangling uuid, got: %v", err)
	}

	// A last-prompt trailer pointing at a uuid absent from the file.
	dangling := append([][]byte(nil), lines...)
	dangling[7] = bytes.Replace(dangling[7],
		[]byte("643c385b-859b-4a40-a97c-9d547fd897ea"),
		[]byte("ffffffff-ffff-4fff-8fff-ffffffffffff"), 1)
	if err := lintBytes(t, joinLines(dangling)); err == nil {
		t.Fatal("lint must catch a dangling last-prompt leafUuid")
	}

	// A file-history-snapshot whose messageId's user entry is gone.
	blines := fixtureLines(t, "branched.jsonl")
	noUser := make([][]byte, 0, len(blines))
	for i, l := range blines {
		if i == 5 { // line 6: user 20a9d984, referenced by fhs line 5
			continue
		}
		noUser = append(noUser, l)
	}
	err = lintBytes(t, joinLines(noUser))
	if err == nil {
		t.Fatal("lint must catch a dangling file-history-snapshot messageId")
	}
	if !strings.Contains(err.Error(), "20a9d984") {
		t.Errorf("lint error should name the dangling uuid, got: %v", err)
	}

	// A compact boundary whose logicalParentUuid target is gone.
	mlines := fixtureLines(t, "multi-compact.jsonl")
	noLogical := make([][]byte, 0, len(mlines))
	for i, l := range mlines {
		if i == 28 { // line 29: assistant 191bbbeb, boundary #1's logical parent
			continue
		}
		noLogical = append(noLogical, l)
	}
	if err := lintBytes(t, joinLines(noLogical)); err == nil {
		t.Fatal("lint must catch a dangling logicalParentUuid")
	}
}

func TestLintMalformed(t *testing.T) {
	if err := lintBytes(t, []byte("{}\n{nope\n")); err == nil {
		t.Fatal("lint must propagate parse errors")
	} else if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("lint parse error should name the line, got: %v", err)
	}
}
