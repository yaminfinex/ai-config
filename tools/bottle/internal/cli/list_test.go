package cli

import (
	"strings"
	"testing"
)

func TestListEmptyStoreIsPolite(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newDeps(t, st)

	if code := cmdList(d, nil); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("empty store wrote to stderr: %s", stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "No bottles yet") {
		t.Errorf("empty store output not polite: %q", out)
	}
}

func TestListTable(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "the first one", nil)
	makeBottle(t, st, "beta", "", nil)
	makeBottle(t, st, "beta", "", nil) // beta@2

	d, stdout, stderr := newDeps(t, st)
	if code := cmdList(d, nil); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	out := stdout.String()

	for _, want := range []string{
		"NAME", "LATEST", "VERSIONS", "AGE", "NOTE",
		"alpha", "the first one",
		"beta",
		"2d", // nowAt is two days after createdAt
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}

	// beta is sorted before nothing-after; alpha before beta.
	if i, j := strings.Index(out, "alpha"), strings.Index(out, "beta"); i < 0 || j < 0 || i > j {
		t.Errorf("rows not sorted alpha<beta:\n%s", out)
	}

	// beta has two versions, alpha one — assert beta's row shows latest @2
	// and a version count of 2 (columns: NAME LATEST VERSIONS AGE …).
	var betaLine string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(l, "beta") {
			betaLine = l
		}
	}
	if betaLine == "" {
		t.Fatalf("no beta row in output:\n%s", out)
	}
	fields := strings.Fields(betaLine)
	if len(fields) < 3 || fields[1] != "@2" || fields[2] != "2" {
		t.Errorf("beta row latest/count wrong, got fields %v:\n%s", fields, out)
	}
}
