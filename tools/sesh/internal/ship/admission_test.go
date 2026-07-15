package ship

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestHintAdmissionStartsImmediatelyAfterIdle(t *testing.T) {
	interval := 2 * time.Second
	start := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	a := newHintAdmission(interval)

	a.Hint(start)
	deadline, ok := a.Next(start)
	if !ok || !deadline.Equal(start) {
		t.Fatalf("first idle hint deadline = %v, ok=%v; want immediate %v", deadline, ok, start)
	}
	if !a.Take(start) {
		t.Fatal("first idle hint must admit a pass immediately")
	}

	idleHint := start.Add(2 * interval)
	a.Hint(idleHint)
	deadline, ok = a.Next(idleHint)
	if !ok || !deadline.Equal(idleHint) {
		t.Fatalf("hint after idle deadline = %v, ok=%v; want immediate %v", deadline, ok, idleHint)
	}
}

func TestHintAdmissionCoalescesBurstDuringCooldown(t *testing.T) {
	interval := 2 * time.Second
	start := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	a := newHintAdmission(interval)
	a.Hint(start)
	a.Take(start)

	for _, offset := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 500 * time.Millisecond, time.Second} {
		a.Hint(start.Add(offset))
	}
	deadline, ok := a.Next(start.Add(time.Second))
	want := start.Add(interval)
	if !ok || !deadline.Equal(want) {
		t.Fatalf("coalesced burst deadline = %v, ok=%v; want one pending deadline %v", deadline, ok, want)
	}
	if a.Take(want.Add(-time.Nanosecond)) {
		t.Fatal("cooldown hint admitted before the start-to-start interval")
	}
	if !a.Take(want) {
		t.Fatal("coalesced cooldown hint did not admit at its single deadline")
	}
	if _, ok := a.Next(want); ok {
		t.Fatal("burst built more than one pending pass")
	}
}

func TestHintAdmissionContinuousStartsStaySpaced(t *testing.T) {
	interval := 2 * time.Second
	start := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	a := newHintAdmission(interval)
	var starts []time.Time
	for now := start; now.Before(start.Add(12 * time.Second)); now = now.Add(20 * time.Millisecond) {
		a.Hint(now)
		if deadline, ok := a.Next(now); ok && !deadline.After(now) && a.Take(now) {
			starts = append(starts, now)
		}
	}
	if len(starts) < 5 {
		t.Fatalf("continuous admissions = %d, want at least 5", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if elapsed := starts[i].Sub(starts[i-1]); elapsed < interval {
			t.Fatalf("starts %d and %d are %s apart, want at least %s", i-1, i, elapsed, interval)
		}
	}
}

func TestPeriodicAdmissionConsumesPendingHint(t *testing.T) {
	interval := 2 * time.Second
	start := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	a := newHintAdmission(interval)
	a.Hint(start)
	a.Take(start)
	a.Hint(start.Add(500 * time.Millisecond))

	tick := start.Add(time.Second)
	a.Periodic()
	deadline, ok := a.Next(tick)
	if !ok || !deadline.Equal(tick) || !a.Take(tick) {
		t.Fatalf("periodic race deadline = %v, ok=%v; want one pass at %v", deadline, ok, tick)
	}
	if _, ok := a.Next(start.Add(interval)); ok {
		t.Fatal("periodic pass left a stale hint pass pending")
	}
}

func TestPeriodicAdmissionRunsRegistrationCallback(t *testing.T) {
	a := newHintAdmission(2 * time.Second)
	a.Hint(time.Now()) // due at the same time as the buffered tick
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	registered := 0
	if err := waitForAdmission(context.Background(), ticks, nil, a, func() { registered++ }); err != nil {
		t.Fatal(err)
	}
	if registered != 1 {
		t.Fatalf("periodic registration callbacks = %d, want 1", registered)
	}
	if _, ok := a.Next(time.Now()); ok {
		t.Fatal("simultaneously-ready tick and hint produced more than one admission")
	}
}

func TestPeriodicWatchRewalkRegistersNestedDirectory(t *testing.T) {
	fixtureRoot := t.TempDir()
	realBase := filepath.Join(fixtureRoot, "real")
	linkedBase := filepath.Join(fixtureRoot, "linked")
	if err := os.Mkdir(realBase, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBase, linkedBase); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(linkedBase, "shipper-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	s := &Shipper{Roots: Roots{
		Claude: filepath.Join(base, "claude"),
		Codex:  filepath.Join(base, "codex"),
		Grok:   filepath.Join(base, "missing-grok"),
		Pi:     filepath.Join(base, "missing-pi"),
	}, Rescan: 10 * time.Millisecond}
	if err := os.MkdirAll(s.Roots.Claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Roots.Codex, 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := OpenRegistry(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.Close)
	s.Registry = reg
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.runWithWatcher(ctx, w) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("shipper shutdown: %v", err)
		}
	})

	resolvedRoot, err := filepath.EvalSymlinks(s.Roots.Codex)
	if err != nil {
		t.Fatal(err)
	}
	waitForWatch(t, w, resolvedRoot)

	stagedYear := filepath.Join(fixtureRoot, "staged", "2026")
	stagedNested := filepath.Join(stagedYear, "07", "10")
	if err := os.MkdirAll(stagedNested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stagedYear, filepath.Join(s.Roots.Codex, "2026")); err != nil {
		t.Fatal(err)
	}
	unresolvedNested := filepath.Join(s.Roots.Codex, "2026", "07", "10")
	resolvedNested, err := filepath.EvalSymlinks(unresolvedNested)
	if err != nil {
		t.Fatal(err)
	}
	if unresolvedNested == resolvedNested {
		t.Fatalf("fixture does not exercise distinct path spellings: %s", unresolvedNested)
	}
	// Moving a prebuilt tree generates at most a Create hint for its top
	// directory. Only the production periodic rewalk registers its descendants.
	waitForWatch(t, w, resolvedNested)
}

func waitForWatch(t *testing.T, w *fsnotify.Watcher, path string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		if watched := w.WatchList(); slices.Contains(watched, path) {
			return
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			t.Fatalf("periodic watch rewalk did not register %s; watches: %v", path, w.WatchList())
		}
	}
}

func TestHintAdmissionHonorsBackoffAndCancellation(t *testing.T) {
	start := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	a := newHintAdmission(2 * time.Second)
	a.Hint(start)
	backoffUntil := start.Add(5 * time.Second)
	a.HoldUntil(backoffUntil)
	deadline, ok := a.Next(start)
	if !ok || !deadline.Equal(backoffUntil) {
		t.Fatalf("backed-off deadline = %v, ok=%v; want %v", deadline, ok, backoffUntil)
	}
	if a.Take(backoffUntil.Add(-time.Nanosecond)) {
		t.Fatal("hint bypassed store backoff")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dueNow := newHintAdmission(2 * time.Second)
	dueNow.Hint(time.Now())
	err := waitForAdmission(ctx, nil, nil, dueNow, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown wait error = %v, want context canceled", err)
	}
}

func TestAdaptiveAdmissionKeepsSustainedPassCountWithinFixedCeiling(t *testing.T) {
	interval := 2 * time.Second
	window := 60 * time.Second
	step := 20 * time.Millisecond
	start := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	a := newHintAdmission(interval)
	adaptive := 0
	for now := start; now.Before(start.Add(window)); now = now.Add(step) {
		a.Hint(now)
		if deadline, ok := a.Next(now); ok && !deadline.After(now) && a.Take(now) {
			adaptive++
		}
	}
	fixed := int(window / interval)
	t.Logf("continuous 60s admission count: adaptive=%d fixed=%d", adaptive, fixed)
	if float64(adaptive) > float64(fixed)*1.10 {
		t.Fatalf("adaptive sustained admissions = %d, fixed = %d; exceeds 10%% CPU proxy", adaptive, fixed)
	}
}
