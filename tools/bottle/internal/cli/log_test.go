package cli

import (
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/refs"
	"ai-config/tools/bottle/internal/store"
)

func TestLogChainWithProvenance(t *testing.T) {
	st := openTestStore(t)
	b1 := makeBottle(t, st, "beta", "root context", nil)
	sess := "sess-abcdef01-2345"
	if err := st.RecordDecant(sess, b1.ID); err != nil {
		t.Fatalf("record decant: %v", err)
	}
	makeBottle(t, st, "beta", "rebottled", &store.Parent{BottleID: b1.ID, DecantSessionID: sess})

	d, stdout, stderr := newDeps(t, st)
	if code := cmdLog(d, []string{"beta"}); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	out := stdout.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 log lines, got %d:\n%s", len(lines), out)
	}
	// Newest first.
	if !strings.HasPrefix(lines[0], "@2") {
		t.Errorf("first line not @2 (newest first):\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "@1") {
		t.Errorf("second line not @1:\n%s", out)
	}
	if !strings.Contains(lines[0], "← decant of beta@1") {
		t.Errorf("@2 line missing parent provenance:\n%s", lines[0])
	}
	if !strings.Contains(lines[0], "session "+shortSession(sess)) {
		t.Errorf("@2 line missing decant session:\n%s", lines[0])
	}
	if !strings.Contains(lines[0], "2026-06-08") {
		t.Errorf("@2 line missing creation date:\n%s", lines[0])
	}
	if !strings.Contains(lines[1], "root context") {
		t.Errorf("@1 line missing note:\n%s", lines[1])
	}
}

func TestLogDeletedParentRendersDeleted(t *testing.T) {
	st := openTestStore(t)
	b1 := makeBottle(t, st, "beta", "", nil)
	makeBottle(t, st, "beta", "", &store.Parent{BottleID: b1.ID, DecantSessionID: "sess-xyz"})

	// rm beta@1 — held as @2's parent, allowed, log should show (deleted).
	if err := st.Remove(refs.Ref{Name: "beta", Version: 1}); err != nil {
		t.Fatalf("remove beta@1: %v", err)
	}

	d, stdout, stderr := newDeps(t, st)
	if code := cmdLog(d, []string{"beta"}); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "← decant of (deleted)") {
		t.Errorf("log does not render deleted parent:\n%s", out)
	}
}

func TestLogFlags(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.Create(store.CreateRequest{
		Name:              "gamma",
		Transcript:        []byte("{}\n"),
		Compacted:         true,
		RewoundIntoParent: true,
		Source:            store.Source{SessionID: "s", Harness: "claude", CWD: "/tmp"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	d, stdout, stderr := newDeps(t, st)
	if code := cmdLog(d, []string{"gamma"}); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "[compacted]") || !strings.Contains(out, "[rewound-into-parent]") {
		t.Errorf("log missing flag tags:\n%s", out)
	}
}

func TestLogUnknownNameErrors(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newDeps(t, st)
	if code := cmdLog(d, []string{"nope"}); code == 0 {
		t.Fatalf("exit code = 0, want non-zero for unknown name")
	}
	if stderr.Len() == 0 {
		t.Errorf("no error written for unknown name")
	}
}
