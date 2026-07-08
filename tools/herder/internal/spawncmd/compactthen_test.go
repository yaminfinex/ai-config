package spawncmd

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fakeProbe scripts a sequence of status samples and records deliver calls, so
// runThenLoop's turn-end detection and delivery handling are testable without a
// live bus or wall-clock sleeps.
type fakeProbe struct {
	statuses    []string
	idx         int
	deliverRet  []string
	deliverN    int
	lastMessage string
	// Event-history proof: the arm-time watermark and the id of a post-arm
	// turn-end event (0 = no such event exists). snapshotFailed makes the arm
	// watermark UNESTABLISHED (ok=false), disabling proof (b).
	armWatermark   int64
	turnEndEventID int64
	snapshotFailed bool
}

func (f *fakeProbe) listStatus(_, _ string) string {
	if f.idx < len(f.statuses) {
		s := f.statuses[f.idx]
		f.idx++
		return s
	}
	// Hold the last scripted status once exhausted.
	if len(f.statuses) == 0 {
		return ""
	}
	return f.statuses[len(f.statuses)-1]
}

func (f *fakeProbe) maxEventID(_, _ string) (int64, bool) {
	if f.snapshotFailed {
		return 0, false
	}
	return f.armWatermark, true
}

func (f *fakeProbe) turnEndedSince(_, _ string, watermark int64) bool {
	return f.turnEndEventID != 0 && f.turnEndEventID > watermark
}

func (f *fakeProbe) deliver(_, _, message string, _ int) string {
	f.lastMessage = message
	ret := "send_failed"
	if f.deliverN < len(f.deliverRet) {
		ret = f.deliverRet[f.deliverN]
	} else if len(f.deliverRet) > 0 {
		ret = f.deliverRet[len(f.deliverRet)-1]
	}
	f.deliverN++
	return ret
}

// fakeClock advances a virtual now by a fixed step on every sleep, so timeout
// and grace windows are deterministic with no real waiting.
type fakeClock struct {
	now  time.Time
	step time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(d time.Duration) {
	step := c.step
	if step <= 0 {
		step = d
	}
	c.now = c.now.Add(step)
}

func baseCfg() thenConfig {
	return thenConfig{
		BusName:        "me-bus",
		BusDir:         "",
		Message:        "continue: run the gate, then report DONE",
		PollMS:         100,
		TimeoutMS:      10000,
		RetryBackoffMS: 100,
		DeliverdMS:     3000,
	}
}

func TestThenLoopWaitsThroughActiveThenDelivers(t *testing.T) {
	p := &fakeProbe{
		statuses:   []string{"active", "active", "listening"},
		deliverRet: []string{"delivered"},
	}
	clk := &fakeClock{now: time.Unix(0, 0), step: 100 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, baseCfg(), &log, clk.Now, clk.Sleep)

	if code != 0 {
		t.Fatalf("want exit 0, got %d; log:\n%s", code, log.String())
	}
	if p.deliverN != 1 {
		t.Fatalf("want exactly 1 deliver, got %d", p.deliverN)
	}
	if p.lastMessage != baseCfg().Message {
		t.Fatalf("delivered wrong message: %q", p.lastMessage)
	}
	if !strings.Contains(log.String(), "turn end PROVEN (observed working→listening transition)") {
		t.Fatalf("log missing turn-end line:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "delivered") {
		t.Fatalf("log missing delivered line:\n%s", log.String())
	}
}

func TestThenLoopNeverDeliversWhileActive(t *testing.T) {
	// If it delivered on any "active" sample, deliverN would be >0 before the
	// listening sample. Script a long active run then listening.
	p := &fakeProbe{
		statuses:   []string{"active", "active", "active", "active", "listening"},
		deliverRet: []string{"delivered"},
	}
	clk := &fakeClock{now: time.Unix(0, 0), step: 100 * time.Millisecond}
	var log bytes.Buffer
	runThenLoop(p, baseCfg(), &log, clk.Now, clk.Sleep)

	// The only deliver must have happened after all active samples were consumed.
	if p.deliverN != 1 {
		t.Fatalf("want 1 deliver after the active run, got %d", p.deliverN)
	}
	if p.idx < 5 {
		t.Fatalf("delivered before consuming the active run (idx=%d)", p.idx)
	}
}

func TestThenLoopArmedLateDeliversOnEventProof(t *testing.T) {
	// Armed-late: the turn ended before the first poll, so "active" is never
	// sampled — but hcom's event history carries a status-listening event newer
	// than the arm watermark, which PROVES the post-arm transition. It must
	// deliver on that proof (proof (b)), not on the naked sample.
	p := &fakeProbe{
		statuses:       []string{"listening", "listening"},
		deliverRet:     []string{"delivered"},
		armWatermark:   100,
		turnEndEventID: 150, // > watermark → post-arm turn end proven
	}
	clk := &fakeClock{now: time.Unix(0, 0), step: 100 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, baseCfg(), &log, clk.Now, clk.Sleep)

	if code != 0 {
		t.Fatalf("want exit 0 on event proof, got %d; log:\n%s", code, log.String())
	}
	if p.deliverN != 1 {
		t.Fatalf("want 1 deliver via event proof, got %d", p.deliverN)
	}
	if !strings.Contains(log.String(), "hcom events show a post-arm") {
		t.Fatalf("log missing event-proof line:\n%s", log.String())
	}
}

func TestThenLoopPoisonNakedListeningNeverDelivers(t *testing.T) {
	// The P1 poison case: sampled "listening" forever, NO observed active→
	// listening transition, and NO post-arm turn-end event (the listening event,
	// if any, predates the arm watermark). A naked sample must NEVER suffice —
	// it must fail closed at timeout and deliver nothing.
	cases := []struct {
		name           string
		turnEndEventID int64
	}{
		{"no turn-end event at all", 0},
		{"turn-end event predates arm watermark", 50}, // < watermark 100
	}
	for _, c := range cases {
		p := &fakeProbe{
			statuses:       []string{"listening"},
			deliverRet:     []string{"delivered"},
			armWatermark:   100,
			turnEndEventID: c.turnEndEventID,
		}
		cfg := baseCfg()
		cfg.PollMS = 1000
		cfg.TimeoutMS = 5000
		clk := &fakeClock{now: time.Unix(0, 0), step: 1000 * time.Millisecond}
		var log bytes.Buffer
		code := runThenLoop(p, cfg, &log, clk.Now, clk.Sleep)

		if code != 1 {
			t.Fatalf("%s: want exit 1 (fail closed), got %d; log:\n%s", c.name, code, log.String())
		}
		if p.deliverN != 0 {
			t.Fatalf("%s: naked listening must NOT deliver, got %d delivers", c.name, p.deliverN)
		}
		if !strings.Contains(log.String(), "FAILING CLOSED") {
			t.Fatalf("%s: log missing fail-closed line:\n%s", c.name, log.String())
		}
	}
}

func TestThenLoopFailedSnapshotDisablesEventProof(t *testing.T) {
	// codex review P1 residual (fail-open): the arm-time snapshot FAILED, so the
	// watermark is untrusted. Even though a "listening" event exists and the
	// sampled status is listening, proof (b) MUST be disabled — that event could
	// predate the arm. With no observed active→listening transition either, it
	// must fail closed and deliver nothing.
	p := &fakeProbe{
		statuses:       []string{"listening"},
		deliverRet:     []string{"delivered"},
		snapshotFailed: true,
		turnEndEventID: 999, // a listening event exists, but the watermark is untrusted
	}
	cfg := baseCfg()
	cfg.PollMS = 1000
	cfg.TimeoutMS = 5000
	clk := &fakeClock{now: time.Unix(0, 0), step: 1000 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, cfg, &log, clk.Now, clk.Sleep)

	if code != 1 {
		t.Fatalf("failed snapshot + stale listening must fail closed (exit 1), got %d; log:\n%s", code, log.String())
	}
	if p.deliverN != 0 {
		t.Fatalf("must NOT deliver on an untrusted watermark, got %d delivers", p.deliverN)
	}
	if !strings.Contains(log.String(), "event-history proof DISABLED") {
		t.Fatalf("log missing proof-disabled line:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "event_proof=false") {
		t.Fatalf("timeout line should record event_proof=false:\n%s", log.String())
	}
}

func TestThenLoopFailedSnapshotStillDeliversOnObservedTransition(t *testing.T) {
	// Even with a failed snapshot, proof (a) — a live observed active→listening
	// transition — remains a valid path (it needs no watermark).
	p := &fakeProbe{
		statuses:       []string{"active", "listening"},
		deliverRet:     []string{"delivered"},
		snapshotFailed: true,
	}
	clk := &fakeClock{now: time.Unix(0, 0), step: 100 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, baseCfg(), &log, clk.Now, clk.Sleep)

	if code != 0 {
		t.Fatalf("observed transition must still deliver despite failed snapshot, got %d; log:\n%s", code, log.String())
	}
	if p.deliverN != 1 {
		t.Fatalf("want 1 deliver via proof (a), got %d", p.deliverN)
	}
}

func TestThenLoopTimesOutLoudly(t *testing.T) {
	// Never leaves "active": the turn never ends. Must time out with a loud line
	// and the manual remedy, never deliver.
	p := &fakeProbe{
		statuses:   []string{"active"},
		deliverRet: []string{"delivered"},
	}
	cfg := baseCfg()
	cfg.PollMS = 1000
	cfg.TimeoutMS = 5000
	clk := &fakeClock{now: time.Unix(0, 0), step: 1000 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, cfg, &log, clk.Now, clk.Sleep)

	if code != 1 {
		t.Fatalf("want exit 1 on timeout, got %d; log:\n%s", code, log.String())
	}
	if p.deliverN != 0 {
		t.Fatalf("must not deliver on timeout, got %d delivers", p.deliverN)
	}
	if !strings.Contains(log.String(), "TIMEOUT") || !strings.Contains(log.String(), "herder send me-bus") {
		t.Fatalf("log missing timeout+remedy:\n%s", log.String())
	}
}

func TestThenLoopQueuedIsSuccess(t *testing.T) {
	p := &fakeProbe{
		statuses:   []string{"active", "listening"},
		deliverRet: []string{"queued"},
	}
	clk := &fakeClock{now: time.Unix(0, 0), step: 100 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, baseCfg(), &log, clk.Now, clk.Sleep)

	if code != 0 {
		t.Fatalf("queued must be success (exit 0), got %d", code)
	}
	if !strings.Contains(log.String(), "queued") || !strings.Contains(log.String(), "NOT resending") {
		t.Fatalf("log missing queued/do-not-resend:\n%s", log.String())
	}
}

func TestThenLoopRetriesTransientThenDelivers(t *testing.T) {
	// Transient not_joined/send_failed retries with a settling backoff and
	// eventually delivers — well within the timeout budget.
	p := &fakeProbe{
		statuses:   []string{"active", "listening"},
		deliverRet: []string{"not_joined", "send_failed", "delivered"},
	}
	clk := &fakeClock{now: time.Unix(0, 0), step: 100 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, baseCfg(), &log, clk.Now, clk.Sleep)

	if code != 0 {
		t.Fatalf("want eventual delivery (exit 0), got %d; log:\n%s", code, log.String())
	}
	if p.deliverN != 3 {
		t.Fatalf("want 3 deliver attempts, got %d", p.deliverN)
	}
	if !strings.Contains(log.String(), "retrying in 100ms") {
		t.Fatalf("log missing backoff line:\n%s", log.String())
	}
}

func TestThenLoopGivesUpWhenBudgetExhausted(t *testing.T) {
	// The send never succeeds: retries spend the REMAINING budget (not a fixed
	// count), each attempt logged, then fail closed with the manual remedy once
	// the deadline passes.
	p := &fakeProbe{
		statuses:   []string{"active", "listening"},
		deliverRet: []string{"send_failed"},
	}
	cfg := baseCfg()
	cfg.TimeoutMS = 500 // small budget; backoff 100ms → a few attempts then give up
	clk := &fakeClock{now: time.Unix(0, 0), step: 100 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, cfg, &log, clk.Now, clk.Sleep)

	if code != 1 {
		t.Fatalf("want exit 1 when budget exhausted, got %d", code)
	}
	if p.deliverN < 2 {
		t.Fatalf("want multiple retry attempts, got %d", p.deliverN)
	}
	if !strings.Contains(log.String(), "FAILED to deliver within the --then-timeout budget") ||
		!strings.Contains(log.String(), "herder send me-bus") {
		t.Fatalf("log missing budget-exhausted give-up + remedy:\n%s", log.String())
	}
}

func TestPickStatusShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		// Live scoped-query shape: a single object whose name is the BASE name,
		// not the full bus name queried with.
		{"single object base name", `{"name":"reko","status":"listening"}`, "listening"},
		{"single object active", `{"name":"reko","status":"active"}`, "active"},
		// Defensive array shapes.
		{"sole array element", `[{"name":"reko","status":"listening"}]`, "listening"},
		{"array match by full name", `[{"name":"other","status":"active"},{"name":"smoke034-reko","status":"listening"}]`, "listening"},
		{"array match by base name", `[{"name":"x","status":"active"},{"base_name":"reko","name":"smoke034-reko","status":"blocked"}]`, "blocked"},
		{"empty", ``, ""},
		{"empty array", `[]`, ""},
		{"garbage", `not json`, ""},
	}
	for _, c := range cases {
		if got := pickStatus([]byte(c.raw), "smoke034-reko"); got != c.want {
			t.Errorf("%s: pickStatus(%q) = %q, want %q", c.name, c.raw, got, c.want)
		}
	}
}

func TestParseThenArgsRequiresNameAndMessage(t *testing.T) {
	var errb bytes.Buffer
	if _, code := parseThenArgs([]string{"--name", "me-bus"}, &errb); code != 64 {
		t.Fatalf("want usage exit 64 without --message, got %d", code)
	}
	errb.Reset()
	cfg, code := parseThenArgs([]string{"--name", "me-bus", "--message", "go", "--timeout-ms", "1234", "--poll-ms", "7"}, &errb)
	if code != 0 {
		t.Fatalf("want exit 0, got %d (%s)", code, errb.String())
	}
	if cfg.BusName != "me-bus" || cfg.Message != "go" || cfg.TimeoutMS != 1234 || cfg.PollMS != 7 {
		t.Fatalf("parsed cfg wrong: %+v", cfg)
	}
}
