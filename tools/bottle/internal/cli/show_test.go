package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/store"
	"ai-config/tools/bottle/internal/transcript"
)

// seedFromFixture creates a bottle whose transcript is a real fixture, so
// show's preview exercises the transcript package against actual CC JSONL.
func seedFromFixture(t *testing.T, st *store.Store, name, fixture string) ([]byte, *store.Bottle) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + fixture)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	b, err := st.Create(store.CreateRequest{
		Name:       name,
		Transcript: data,
		Note:       "fixture-backed",
		Source: store.Source{
			SessionID:  "00000000-0000-4000-8000-000000000001",
			Harness:    "claude",
			CWD:        "/tmp/bottle-u1-lab",
			GitBranch:  "main",
			GitSHA:     "deadbeef",
			CutTurn:    3,
			TotalTurns: 3,
		},
	})
	if err != nil {
		t.Fatalf("create from fixture: %v", err)
	}
	return data, b
}

func TestShowMetadataAndPreview(t *testing.T) {
	st := openTestStore(t)
	data, _ := seedFromFixture(t, st, "alpha", "plain.jsonl")

	info, err := transcript.Index(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("index fixture: %v", err)
	}
	allTurns := info.Turns()
	if len(allTurns) == 0 {
		t.Fatalf("fixture has no turns; pick a different fixture")
	}

	d, stdout, stderr := newDeps(t, st)
	if code := cmdShow(d, []string{"alpha"}); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	out := stdout.String()

	for _, want := range []string{
		"alpha@1",
		"fixture-backed",
		"harness claude",
		"/tmp/bottle-u1-lab",
		"main @ deadbeef",
		"cut at 3 of 3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}

	// The last turn's text must appear in the preview.
	last := allTurns[len(allTurns)-1]
	if snippet := oneLine(last.Text, 80); snippet != "" && !strings.Contains(out, snippet) {
		t.Errorf("preview missing last turn text %q:\n%s", snippet, out)
	}
}

func TestShowTurnsFlagLimitsPreview(t *testing.T) {
	st := openTestStore(t)
	data, _ := seedFromFixture(t, st, "alpha", "plain.jsonl")
	info, _ := transcript.Index(bytes.NewReader(data))
	total := len(info.Turns())

	d, stdout, stderr := newDeps(t, st)
	if code := cmdShow(d, []string{"alpha", "--turns", "1"}); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	out := stdout.String()
	want := "last 1 turn(s) of " // count clamps to min(1, total)
	if total >= 1 && !strings.Contains(out, want) {
		t.Errorf("--turns 1 did not limit preview header:\n%s", out)
	}
}

func TestShowUnknownErrors(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newDeps(t, st)
	if code := cmdShow(d, []string{"missing"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero for missing bottle")
	}
	if stderr.Len() == 0 {
		t.Errorf("no error written for missing bottle")
	}
}
