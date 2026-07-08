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
		BusName:    "me-bus",
		BusDir:     "",
		Message:    "continue: run the gate, then report DONE",
		PollMS:     100,
		TimeoutMS:  10000,
		GraceMS:    4000,
		DeliverdMS: 3000,
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
	if !strings.Contains(log.String(), "turn ended (status=listening after working)") {
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

func TestThenLoopGraceFiresWithoutObservedActive(t *testing.T) {
	// Turn ended before the first poll could catch "active": only ever see
	// listening. It must NOT fire immediately (grace guards a stale sample) but
	// must fire once the grace window elapses.
	p := &fakeProbe{
		statuses:   []string{"listening", "listening", "listening", "listening", "listening", "listening"},
		deliverRet: []string{"delivered"},
	}
	cfg := baseCfg()
	cfg.PollMS = 1000
	cfg.GraceMS = 3000
	clk := &fakeClock{now: time.Unix(0, 0), step: 1000 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, cfg, &log, clk.Now, clk.Sleep)

	if code != 0 {
		t.Fatalf("want exit 0, got %d; log:\n%s", code, log.String())
	}
	if p.deliverN != 1 {
		t.Fatalf("want 1 deliver via grace, got %d", p.deliverN)
	}
	if p.idx < 3 {
		t.Fatalf("fired before the grace window elapsed (idx=%d)", p.idx)
	}
	if !strings.Contains(log.String(), "via grace window") {
		t.Fatalf("log missing grace-window line:\n%s", log.String())
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
}

func TestThenLoopGivesUpAfterRetries(t *testing.T) {
	p := &fakeProbe{
		statuses:   []string{"active", "listening"},
		deliverRet: []string{"send_failed"},
	}
	clk := &fakeClock{now: time.Unix(0, 0), step: 100 * time.Millisecond}
	var log bytes.Buffer
	code := runThenLoop(p, baseCfg(), &log, clk.Now, clk.Sleep)

	if code != 1 {
		t.Fatalf("want exit 1 after exhausting retries, got %d", code)
	}
	if p.deliverN != 5 {
		t.Fatalf("want 5 attempts, got %d", p.deliverN)
	}
	if !strings.Contains(log.String(), "FAILED to deliver after 5 attempts") {
		t.Fatalf("log missing give-up line:\n%s", log.String())
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
