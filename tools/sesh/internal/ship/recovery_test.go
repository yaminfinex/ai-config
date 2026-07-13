package ship

// Regression coverage for the fresh-registry recovery pass over a large
// corpus: every discovered identity costs one store round trip, so an
// interrupted pass must resume behind the identities the store already
// answered — restarting from zero on every hold can keep a shipper on a
// flaky link from ever finishing recovery.

import (
	"context"
	"errors"
	"testing"
)

func TestInterruptedRecoveryPassResumes(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	// Lexically ordered so the walk answers `first` before holding on `second`.
	first := "10000000-0000-4000-8000-000000000000"
	second := "20000000-0000-4000-8000-000000000000"
	h.writeClaude("proj", first, data)
	h.writeClaude("proj", second, data)
	h.store.recoveryUnavailableFor = map[string]bool{h.store.key("claude", second, second): true}

	if err := h.shipper.RunOnce(context.Background()); !errors.Is(err, errHold) {
		t.Fatalf("RunOnce with recovery held = %v, want errHold", err)
	}
	if got := h.store.recoveries("claude", first, first); got != 1 {
		t.Fatalf("recovery GETs for the answered identity after the held pass = %d, want 1", got)
	}

	h.store.recoveryUnavailableFor = nil
	h.runOnce()
	if got := h.store.recoveries("claude", first, first); got != 1 {
		t.Fatalf("recovery GETs for the answered identity after the resumed pass = %d, want 1 — the pass must resume, not restart", got)
	}
	if got := h.store.recoveries("claude", second, second); got != 2 {
		t.Fatalf("recovery GETs for the held identity = %d, want exactly the retry", got)
	}
	h.assertMirror("claude", first, data)
	h.assertMirror("claude", second, data)
}
