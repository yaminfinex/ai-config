package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"ai-config/tools/bottle/internal/store"
)

// Two clocks two days apart so list/log render a stable, non-zero age and a
// deterministic creation date.
var (
	createdAt = time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	nowAt     = time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
)

// openTestStore opens a store rooted in a temp dir with a fixed creation clock
// and discarded git warnings. Tests never touch ~/.bottles.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir(),
		store.WithClock(func() time.Time { return createdAt }),
		store.WithWarnWriter(io.Discard))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	return st
}

// newDeps builds command deps over st with captured output buffers, a
// non-TTY empty stdin, a "now" clock two days after creation, and an
// always-live sessionExists seam (prune tests override it).
func newDeps(t *testing.T, st *store.Store) (d *deps, stdout, stderr *bytes.Buffer) {
	t.Helper()
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	d = &deps{
		store:         st,
		stdin:         strings.NewReader(""),
		stdout:        stdout,
		stderr:        stderr,
		now:           func() time.Time { return nowAt },
		isTTY:         false,
		sessionExists: func(*store.Meta, string) bool { return true },
	}
	return d, stdout, stderr
}

// makeBottle creates one bottle and returns it, failing the test on error.
func makeBottle(t *testing.T, st *store.Store, name, note string, parent *store.Parent) *store.Bottle {
	t.Helper()
	b, err := st.Create(store.CreateRequest{
		Name:       name,
		Transcript: []byte("{}\n"),
		Note:       note,
		Parent:     parent,
		Source: store.Source{
			SessionID:  "session-" + name,
			Harness:    "claude",
			CWD:        "/tmp/bottle-test",
			CutTurn:    1,
			TotalTurns: 1,
		},
	})
	if err != nil {
		t.Fatalf("create bottle %q: %v", name, err)
	}
	return b
}
