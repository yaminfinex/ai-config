package agentstore

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
)

var herderBin string

// TestMain builds the real herder binary once: the concurrency test spawns
// real `herder register` processes, not goroutines.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "herder-bin-")
	if err != nil {
		panic(err)
	}
	herderBin = filepath.Join(dir, "herder")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", herderBin, "ai-config/tools/herder/cmd/herder")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		panic("build herder: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func scratch(t *testing.T) (string, *Store) {
	t.Helper()
	state := t.TempDir()
	var stderr bytes.Buffer
	s := Open(state, &stderr)
	if s.ImportErr != nil {
		t.Fatalf("open: %v", s.ImportErr)
	}
	return state, s
}

func at(sec int) time.Time { return time.Date(2026, 9, 9, 5, 0, sec, 0, time.UTC) }

func ev(kind, name string, sec int, mutate ...func(*Event)) Event {
	e := Event{ID: NewID(at(sec)), At: at(sec), Kind: kind, By: "ziru", ByKind: "agent", Name: name}
	for _, m := range mutate {
		m(&e)
	}
	return e
}

func mustAppend(t *testing.T, s *Store, e Event) Receipt {
	t.Helper()
	r, err := s.Append(e)
	if err != nil {
		t.Fatalf("append %s: %v", e.Kind, err)
	}
	return r
}

func lines(t *testing.T, path string) [][]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) > 0 {
			out = append(out, line)
		}
	}
	return out
}

func TestFiftyRealRegisterProcessesAppendAtomically(t *testing.T) {
	state := t.TempDir()
	const procs, each = 50, 20
	var wg sync.WaitGroup
	errs := make(chan error, procs)
	for p := 0; p < procs; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				cmd := exec.Command(herderBin, "register", "annotate", "--name", fmt.Sprintf("agent-%02d", p), "--title", fmt.Sprintf("p%02d-i%02d", p, i), "--note", strings.Repeat("x", 200))
				cmd.Env = append(os.Environ(), "HERDER_STATE_DIR="+state, "HCOM_NAME=writer")
				if out, err := cmd.CombinedOutput(); err != nil {
					errs <- fmt.Errorf("p%d i%d: %v: %s", p, i, err, out)
					return
				}
			}
		}(p)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	got := lines(t, filepath.Join(state, "agents", "events.jsonl"))
	if len(got) != procs*each {
		t.Fatalf("lines = %d, want %d", len(got), procs*each)
	}
	ids := map[string]bool{}
	titles := map[string]bool{}
	s := Open(state, nil)
	if err := s.scan(0, func(e Event, _ int64) {
		ids[e.ID] = true
		titles[e.Title] = true
	}); err != nil {
		t.Fatal(err)
	}
	if len(ids) != procs*each || len(titles) != procs*each {
		t.Fatalf("well-formed unique ids = %d, titles = %d, want %d (interleaved or lost lines)", len(ids), len(titles), procs*each)
	}
	for _, line := range got {
		if !bytes.HasPrefix(line, []byte(`{"id":"`)) || !bytes.HasSuffix(line, []byte("}")) {
			t.Fatalf("interleaved line: %.120s", line)
		}
	}
}

func TestReplayingAnIDReturnsSameReceiptAndNoSecondLine(t *testing.T) {
	state, s := scratch(t)
	e := ev(KindAssign, "impl-lima", 1, func(e *Event) { e.Mission = "fleet-refit" })
	first := mustAppend(t, s, e)
	e.Mission = "different-on-retry"
	second := mustAppend(t, s, e)
	if !second.Replayed || second.Offset != first.Offset || second.Event.Mission != "fleet-refit" {
		t.Fatalf("replay receipt = %#v, first = %#v", second, first)
	}
	if n := len(lines(t, filepath.Join(state, "agents", "events.jsonl"))); n != 1 {
		t.Fatalf("lines = %d, want 1", n)
	}
	// The real CLI path: --id makes the retry idempotent too.
	for i := 0; i < 2; i++ {
		cmd := exec.Command(herderBin, "register", "annotate", "--name", "impl-lima", "--title", "t", "--id", "01a084cb-0000-7000-8000-000000000001")
		cmd.Env = append(os.Environ(), "HERDER_STATE_DIR="+state)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cli: %v %s", err, out)
		}
		if i == 1 && !strings.Contains(string(out), "replayed=true") {
			t.Fatalf("second cli run did not report replay: %s", out)
		}
	}
	if n := len(lines(t, filepath.Join(state, "agents", "events.jsonl"))); n != 2 {
		t.Fatalf("lines = %d, want 2", n)
	}
}

func TestSnapshotPlusTailEqualsFullReplayByteForByte(t *testing.T) {
	_, s := scratch(t)
	req := ev(KindLaunchRequested, "", 1, func(e *Event) {
		e.Tool, e.Tag, e.Model, e.Effort = "codex", "impl", "gpt-6", "high"
		e.Placement = &Placement{Workspace: "w80"}
	})
	mustAppend(t, s, req)
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 2, func(e *Event) { e.Request, e.Pane, e.Session, e.Batch = req.ID, "w80:p1", "s-1", "b1" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 3, func(e *Event) { e.Mission = "fleet-refit" }))
	snap, err := s.Load() // writes snapshot at offset after 3 events
	if err != nil || snap.SnapshotErr != nil {
		t.Fatalf("load: %v snapshotErr %v", err, snap.SnapshotErr)
	}
	mustAppend(t, s, ev(KindReparent, "impl-gime", 4, func(e *Event) { e.Manager = "vara" }))
	mustAppend(t, s, ev(KindCulled, "impl-gime", 5, func(e *Event) { e.Pane, e.Close = "w80:p1", "managed" }))
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 6, func(e *Event) { e.Tool = "claude" }))
	tail, err := s.LoadNoSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	full, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := tail.Marshal()
	b, _ := full.Marshal()
	if !bytes.Equal(a, b) {
		t.Fatalf("snapshot+tail != full replay\n%s\n%s", a, b)
	}
	if tail.EventsOffset != full.EventsOffset || len(tail.Agents["impl-gime"]) != 2 {
		t.Fatalf("offset %d/%d incarnations %d", tail.EventsOffset, full.EventsOffset, len(tail.Agents["impl-gime"]))
	}
	// A snapshot pointing past the file (rotation / repair) is ignored.
	if err := os.WriteFile(s.SnapshotPath(), []byte(`{"version":1,"events_offset":999999,"agents":{},"requests":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stale, err := s.LoadNoSnapshot()
	if err != nil || len(stale.Agents["impl-gime"]) != 2 {
		t.Fatalf("stale snapshot not replayed: err=%v agents=%v", err, stale.Agents)
	}
}

func TestReusedNameIsANewIncarnationThatInheritsNothing(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 1, func(e *Event) { e.Pane = "w80:p1"; e.Tool = "codex" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Mission = "old-mission" }))
	mustAppend(t, s, ev(KindReparent, "impl-gime", 3, func(e *Event) { e.Manager = "old-manager" }))
	mustAppend(t, s, ev(KindCulled, "impl-gime", 4, func(e *Event) { e.Pane, e.Close = "w80:p1", "managed" }))
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 10, func(e *Event) { e.By = "vara"; e.Pane = "w81:p1" }))
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	old := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(0)})
	fresh := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(9)})
	latest := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime"})
	if old == nil || old.Assignment == nil || old.Assignment.Mission != "old-mission" || old.Manager != "old-manager" || old.Closed == nil {
		t.Fatalf("old incarnation = %+v", old)
	}
	if fresh == nil || fresh.Assignment != nil || fresh.Manager != "vara" || fresh.Provenance.Launcher != "vara" || fresh.Closed != nil || !fresh.Incarnation.Equal(at(9)) {
		t.Fatalf("fresh incarnation inherited history: %+v", fresh)
	}
	if latest == nil || latest.Provenance.Launcher != "vara" || !latest.Incarnation.Equal(at(10)) {
		t.Fatalf("no roster time should pick the latest: %+v", latest)
	}
	if v := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(4)}); v == nil || v.Manager != "old-manager" {
		t.Fatalf("created at the close instant still maps to the old incarnation: %+v", v)
	}
	if v := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(5)}); v == nil || v.Manager != "vara" {
		t.Fatalf("created after the cull must map to the new incarnation: %+v", v)
	}
}

func TestBindingConflictKeepsRosterSessionCurrent(t *testing.T) {
	raw, err := os.ReadFile("testdata/roster-conflict.json")
	if err != nil {
		t.Fatal(err)
	}
	roster, err := hcomidentity.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 1, func(e *Event) { e.Session = "claimed-S" }))
	mustAppend(t, s, ev(KindLaunchReady, "impl-vara", 1, func(e *Event) { e.Session = "session-vara" }))
	mustAppend(t, s, ev(KindLaunchReady, "impl-pend", 1, func(e *Event) { e.Session = "session-pend" }))
	proj, _ := s.Replay()
	conflict := proj.View("impl-gime", &roster[0])
	if conflict.Binding == nil || conflict.Binding.State != "conflict" || conflict.Binding.Claimed != "claimed-S" || conflict.Binding.Roster != "roster-S-prime" {
		t.Fatalf("binding = %+v", conflict.Binding)
	}
	if conflict.Session == nil || conflict.Session.SessionID != "roster-S-prime" || conflict.Sessions[0].SessionID != "claimed-S" {
		t.Fatalf("roster session must be current and the claim kept as history: %+v %+v", conflict.Session, conflict.Sessions)
	}
	stored, _ := s.Replay()
	if stored.Agents["impl-gime"][0].Sessions[0].SessionID != "claimed-S" || len(stored.Agents["impl-gime"]) != 1 {
		t.Fatal("fold re-keyed the stored record")
	}
	if v := proj.View("impl-vara", &roster[1]); v.Binding == nil || v.Binding.State != "verified" {
		t.Fatalf("verified binding = %+v", v.Binding)
	}
	if v := proj.View("impl-pend", &roster[2]); v.Binding == nil || v.Binding.State != "pending" {
		t.Fatalf("pending binding = %+v", v.Binding)
	}
}

func TestReparentChangesManagerNotLauncher(t *testing.T) {
	_, s := scratch(t)
	req := ev(KindLaunchRequested, "", 1, func(e *Event) { e.Tool, e.Tag = "claude", "impl"; e.Placement = &Placement{Pane: "w1:p1"} })
	mustAppend(t, s, req)
	mustAppend(t, s, ev(KindLaunchReady, "impl-lima", 2, func(e *Event) { e.Request = req.ID; e.By = "spawn-wrapper" }))
	proj, _ := s.Replay()
	v := proj.Latest("impl-lima")
	if v.Provenance.Launcher != "ziru" || v.Manager != "ziru" || v.Provenance.PaneRequested != "w1:p1" {
		t.Fatalf("before reparent: %+v", v)
	}
	mustAppend(t, s, ev(KindReparent, "impl-lima", 3, func(e *Event) { e.Manager, e.By = "vara", "bigboss" }))
	proj, _ = s.Replay()
	v = proj.Latest("impl-lima")
	if v.Provenance.Launcher != "ziru" || v.Manager != "vara" || v.ManagerBy != "bigboss" {
		t.Fatalf("after reparent: launcher=%q manager=%q by=%q", v.Provenance.Launcher, v.Manager, v.ManagerBy)
	}
}

func TestTornTailIsRepairedUnderTheLock(t *testing.T) {
	state, s := scratch(t)
	mustAppend(t, s, ev(KindAnnotate, "a", 1, func(e *Event) { e.Title = "one" }))
	mustAppend(t, s, ev(KindAnnotate, "a", 2, func(e *Event) { e.Title = "two" }))
	path := filepath.Join(state, "agents", "events.jsonl")
	torn := []byte(`{"id":"01a084cb-0000-7000-8000-0000000000ff","at":"2026-09-09T05:00:03Z","kind":"annotate","name":"a","title":"tor`)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.Write(torn)
	f.Close()
	var stderr bytes.Buffer
	s.Stderr = &stderr
	mustAppend(t, s, ev(KindAnnotate, "a", 4, func(e *Event) { e.Title = "three" }))
	got := lines(t, path)
	if len(got) != 3 {
		t.Fatalf("lines = %d, want 3", len(got))
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte(`"tor`)) || bytes.Contains(raw, []byte("0000000000ff")) {
		t.Fatalf("torn bytes survived:\n%s", raw)
	}
	if strings.Count(stderr.String(), "\n") != 1 || !strings.Contains(stderr.String(), "torn tail") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	proj, _ := s.Replay()
	if proj.Latest("a").EventCount != 3 || proj.Latest("a").Annotation.Title != "three" {
		t.Fatalf("count after repair = %d", proj.Latest("a").EventCount)
	}
}

func TestReadersIgnoreTrailingPartialLine(t *testing.T) {
	state, s := scratch(t)
	mustAppend(t, s, ev(KindAnnotate, "a", 1, func(e *Event) { e.Title = "one" }))
	path := filepath.Join(state, "agents", "events.jsonl")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"id":"01a084cb-0000-7000-8000-0000000000ee","kind":"annotate","name":"a","ti`)
	f.Close()
	proj, err := s.Replay()
	if err != nil || proj.Latest("a").EventCount != 1 {
		t.Fatalf("err=%v proj=%+v", err, proj.Agents)
	}
	stat, _ := os.Stat(path)
	if proj.EventsOffset >= stat.Size() {
		t.Fatalf("offset %d should stop before the partial line (size %d)", proj.EventsOffset, stat.Size())
	}
}

func TestOversizeLineIsRefused(t *testing.T) {
	_, s := scratch(t)
	_, err := s.Append(ev(KindAnnotate, "a", 1, func(e *Event) { e.Note = strings.Repeat("n", MaxLineBytes) }))
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("err = %v", err)
	}
}

func TestLockTimeoutIsBoundedAndExits3(t *testing.T) {
	state, s := scratch(t)
	mustAppend(t, s, ev(KindAnnotate, "a", 1, func(e *Event) { e.Title = "one" }))
	holder, err := os.OpenFile(s.EventsPath(), os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := lockFile(holder, time.Second); err != nil {
		t.Fatal(err)
	}
	s.LockTimeout = 100 * time.Millisecond
	start := time.Now()
	_, err = s.Append(ev(KindAnnotate, "a", 2, func(e *Event) { e.Title = "two" }))
	if err == nil || !strings.Contains(err.Error(), "lock timeout") || time.Since(start) > time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
	cmd := exec.Command(herderBin, "register", "annotate", "--name", "a", "--title", "cli")
	cmd.Env = append(os.Environ(), "HERDER_STATE_DIR="+state)
	start = time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 3 || !strings.Contains(string(out), "store unavailable") || elapsed < 2*time.Second || elapsed > 4*time.Second {
		t.Fatalf("cli under held lock: err=%v out=%s elapsed=%s", err, out, elapsed)
	}
}

func TestUnwritableSnapshotStillLoads(t *testing.T) {
	state, s := scratch(t)
	mustAppend(t, s, ev(KindAssign, "a", 1, func(e *Event) { e.Mission = "m" }))
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(state, "agents")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	proj, err := s.Load()
	if err != nil || proj.SnapshotErr == nil || proj.Latest("a") == nil || proj.Latest("a").Assignment.Mission != "m" {
		t.Fatalf("load under read-only dir: err=%v snapshotErr=%v", err, proj.SnapshotErr)
	}
	if _, statErr := os.Stat(s.SnapshotPath()); statErr == nil {
		t.Fatal("snapshot was written into a read-only dir")
	}
}

func TestEdgeImportIsOneTimeAndDeterministic(t *testing.T) {
	state := t.TempDir()
	edge := `{"name":"test-liha","launcher":"web-yamen-core-infinex-gg","tool":"claude","model":"claude-fable-5-1","effort":"high","tag":"test","workspace":"w85","pane":"w85:p4","time":"2026-09-08T22:21:01.047290097Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(state, "launch-edges.jsonl"), []byte(edge), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	s := Open(state, &stderr)
	if s.ImportErr != nil {
		t.Fatal(s.ImportErr)
	}
	proj, _ := s.Replay()
	v := proj.Latest("test-liha")
	if v == nil || v.Provenance.Kind != "registered" || v.Provenance.Launcher != "web-yamen-core-infinex-gg" || v.Provenance.LauncherKind != "web" ||
		v.Provenance.Workspace != "w85" || v.Provenance.Pane != "w85:p4" || v.Provenance.ModelRequested != "claude-fable-5-1" || v.Tool != "claude" || v.Manager != "web-yamen-core-infinex-gg" {
		t.Fatalf("imported view = %+v", v)
	}
	first := lines(t, s.EventsPath())
	// Second open: events.jsonl exists, nothing re-imported.
	Open(state, &stderr)
	if second := lines(t, s.EventsPath()); len(second) != 1 || !bytes.Equal(first[0], second[0]) {
		t.Fatalf("import was not idempotent: %d lines", len(second))
	}
	// Even a forced re-import (events.jsonl removed) yields the same derived id.
	if err := os.Remove(s.EventsPath()); err != nil {
		t.Fatal(err)
	}
	Open(state, &stderr)
	if again := lines(t, s.EventsPath()); !bytes.Equal(first[0], again[0]) {
		t.Fatalf("derived id drifted:\n%s\n%s", first[0], again[0])
	}
	if DerivedID([]byte(edge)) == DerivedID([]byte(edge+" ")) || !ValidID(DerivedID([]byte(edge))) {
		t.Fatal("DerivedID is not a deterministic UUID-shaped function of its input")
	}
}

func TestImportFailureIsRecordedNotFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	state := t.TempDir()
	os.WriteFile(filepath.Join(state, "launch-edges.jsonl"), []byte(`{"name":"x","launcher":"web","time":"2026-09-08T22:21:01Z"}`+"\n"), 0o600)
	os.Chmod(state, 0o500)
	t.Cleanup(func() { os.Chmod(state, 0o700) })
	s := Open(state, nil)
	if s.ImportErr == nil {
		t.Fatal("import into an unwritable state dir did not report")
	}
}

func TestValidateAndIDs(t *testing.T) {
	if err := (Event{ID: NewID(at(0)), At: at(0), Kind: "bogus", Name: "a"}).Validate(); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if err := (Event{ID: NewID(at(0)), At: at(0), Kind: KindCulled, Name: "a", Close: "other"}).Validate(); err == nil {
		t.Fatal("bad --close accepted")
	}
	if err := (Event{ID: NewID(at(0)), At: at(0), Kind: KindLaunchRequested, Tool: "claude"}).Validate(); err == nil {
		t.Fatal("launch-requested without placement accepted")
	}
	a, b := NewID(at(0)), NewID(at(0))
	if a == b || !ValidID(a) || a[14] != '7' || !strings.HasPrefix(a, "01a0") {
		t.Fatalf("ids %s %s", a, b)
	}
	if !strings.HasPrefix(NewID(at(1)), a[:8]) || NewID(at(1)) < a {
		t.Fatal("ids do not sort by time")
	}
}
