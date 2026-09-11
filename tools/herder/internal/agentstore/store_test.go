package agentstore

import (
	"bytes"
	"errors"
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
	e := ev(KindAssign, "impl-lima", 1, func(e *Event) { e.Group = "fleet-refit" })
	first := mustAppend(t, s, e)
	second := mustAppend(t, s, e)
	if !second.Replayed || second.Offset != first.Offset || second.Event.Group != "fleet-refit" {
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
	mustAppend(t, s, ev(KindAssign, "impl-gime", 3, func(e *Event) { e.Group = "fleet-refit" }))
	snap, err := s.Load() // writes snapshot at offset after 3 events
	if err != nil || snap.SnapshotErr != nil {
		t.Fatalf("load: %v snapshotErr %v", err, snap.SnapshotErr)
	}
	s.replays.Store(0)
	mustAppend(t, s, ev(KindAssign, "impl-gime", 4, func(e *Event) { e.Manager = "vara" }))
	mustAppend(t, s, ev(KindCulled, "impl-gime", 5, func(e *Event) { e.Pane, e.Close = "w80:p1", "managed" }))
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 6, func(e *Event) { e.Tool = "claude" }))
	tail, err := s.LoadNoSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if s.replays.Load() != 0 {
		t.Fatalf("clean snapshot tail triggered %d full replay(s)", s.replays.Load())
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

	_, aliasStore := scratch(t)
	aliasReq := ev(KindLaunchRequested, "", 11, func(e *Event) {
		e.Tool, e.Tag, e.By, e.ByKind = "codex", "impl", "ubuntu", "user"
		e.Placement = &Placement{Workspace: "w80"}
	})
	mustAppend(t, aliasStore, aliasReq)
	mustAppend(t, aliasStore, ev(KindLaunchReady, "impl-nife", 12, func(e *Event) {
		e.Request, e.By, e.ByKind = aliasReq.ID, "ubuntu", "user"
	}))
	if snapshot, loadErr := aliasStore.Load(); loadErr != nil || snapshot.SnapshotErr != nil {
		t.Fatalf("alias snapshot: projection=%+v err=%v", snapshot, loadErr)
	}
	aliasStore.replays.Store(0)
	mustAppend(t, aliasStore, ev(KindMirrorReady, "nife", 13, func(e *Event) { e.By, e.ByKind = "ziru", "mirror" }))
	aliasTail, err := aliasStore.LoadNoSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if aliasStore.replays.Load() != 1 {
		t.Fatalf("unaliased mirror tail triggered %d full replay(s), want 1", aliasStore.replays.Load())
	}
	aliasFull, err := aliasStore.Replay()
	if err != nil {
		t.Fatal(err)
	}
	aliasTailJSON, _ := aliasTail.Marshal()
	aliasFullJSON, _ := aliasFull.Marshal()
	if !bytes.Equal(aliasTailJSON, aliasFullJSON) {
		t.Fatalf("alias snapshot+tail != full replay\n%s\n%s", aliasTailJSON, aliasFullJSON)
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

func TestAliasMergesBaseRecord(t *testing.T) {
	state, s := scratch(t)
	fixture, err := os.ReadFile("testdata/live-alias-nife.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.EventsPath(), fixture, 0o600); err != nil {
		t.Fatal(err)
	}

	projection, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if projection.Latest("nife") != nil {
		t.Fatal("base-name alias was retained")
	}
	view := projection.Latest("impl-nife")
	if view == nil || view.Provenance.Kind != "registered" || view.Manager != "ziru" {
		t.Fatalf("merged view = %+v", view)
	}
}

func TestViewForRosterOverlaysUniqueBaseRecord(t *testing.T) {
	created := at(1)
	row := hcomidentity.Row{Name: "sesh-mesa", BaseName: "mesa", Tool: "claude", CreatedAt: created}
	base := Event{ID: NewID(at(2)), At: at(2), Kind: KindMirrorReady, By: "hamo", ByKind: "mirror", Name: "mesa", Batch: "batch-1", ParentName: "parent-1"}
	annotate := Event{ID: NewID(at(3)), At: at(3), Kind: KindAnnotate, By: "web-owner", ByKind: "web", Name: "sesh-mesa", Title: "sesh-measurement"}

	t.Run("full annotation keeps base manager and provenance", func(t *testing.T) {
		projection := NewProjection()
		projection.Apply(base, 0)
		projection.Apply(annotate, 0)
		view := projection.ViewForRoster(&row, []hcomidentity.Row{row})
		if view == nil || view.Manager != "hamo" || view.Annotation == nil || view.Annotation.Title != "sesh-measurement" || view.Provenance.Launcher != "hamo" || view.Provenance.Kind != "mirrored" || view.Provenance.State != "ready" || view.Parent != "parent-1" || view.EventCount != 2 || view.Events[0].Kind != KindMirrorReady {
			t.Fatalf("overlaid view = %+v", view)
		}
	})

	t.Run("later full-name assignment wins", func(t *testing.T) {
		projection := NewProjection()
		projection.Apply(base, 0)
		projection.Apply(annotate, 0)
		projection.Apply(Event{ID: NewID(at(4)), At: at(4), Kind: KindAssign, By: "ziru", ByKind: "agent", Name: "sesh-mesa", Manager: "new-manager"}, 0)
		view := projection.ViewForRoster(&row, []hcomidentity.Row{row})
		if view == nil || view.Manager != "new-manager" || view.ManagerBy != "ziru" || view.ManagerAt == nil || !view.ManagerAt.Equal(at(4)) {
			t.Fatalf("full-name manager lost: %+v", view)
		}
	})

	t.Run("ambiguous base does not overlay", func(t *testing.T) {
		projection := NewProjection()
		projection.Apply(base, 0)
		projection.Apply(annotate, 0)
		roster := []hcomidentity.Row{row, {Name: "other-mesa", BaseName: "mesa", CreatedAt: created}}
		view := projection.ViewForRoster(&row, roster)
		if view == nil || view.Manager != "" || view.Provenance.Kind != "unregistered" || view.Annotation == nil || view.Annotation.Title != "sesh-measurement" {
			t.Fatalf("ambiguous base overlaid: %+v", view)
		}
	})

	t.Run("absent base leaves full view unchanged", func(t *testing.T) {
		projection := NewProjection()
		projection.Apply(annotate, 0)
		view := projection.ViewForRoster(&row, []hcomidentity.Row{row})
		if view == nil || view.Manager != "" || view.Provenance.Kind != "unregistered" || view.Annotation == nil || view.Annotation.Title != "sesh-measurement" {
			t.Fatalf("view without base = %+v", view)
		}
	})
}

func TestAliasDoesNotGreedilyMergeSameBaseAcrossTags(t *testing.T) {
	_, s := scratch(t)
	for i, tagged := range []struct{ name, tag string }{{"impl-nife", "impl"}, {"review-nife", "review"}} {
		req := ev(KindLaunchRequested, "", 1+i, func(e *Event) {
			e.Tool, e.Tag, e.By = "codex", tagged.tag, "ubuntu"
			e.Placement = &Placement{Workspace: "w80"}
		})
		mustAppend(t, s, req)
		mustAppend(t, s, ev(KindLaunchReady, tagged.name, 3+i, func(e *Event) { e.Request, e.By = req.ID, "ubuntu" }))
	}
	mustAppend(t, s, ev(KindMirrorReady, "nife", 5, func(e *Event) { e.By, e.ByKind = "ziru", "mirror" }))

	projection, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if projection.Latest("nife") == nil || projection.Latest("impl-nife") == nil || projection.Latest("review-nife") == nil {
		t.Fatalf("ambiguous base was greedily merged: names=%v", projection.Names())
	}
}

func TestAliasWindowEndLeavesLaterMirrorRecordSeparate(t *testing.T) {
	_, s := scratch(t)
	req := ev(KindLaunchRequested, "", 1, func(e *Event) {
		e.Tool, e.Tag = "codex", "impl"
		e.Placement = &Placement{Workspace: "w80"}
	})
	mustAppend(t, s, req)
	mustAppend(t, s, ev(KindLaunchReady, "impl-nife", 2, func(e *Event) { e.Request = req.ID }))
	mustAppend(t, s, ev(KindCulled, "impl-nife", 4, func(e *Event) { e.Pane, e.Close = "p1", "managed" }))
	mustAppend(t, s, ev(KindMirrorReady, "nife", 5, func(e *Event) { e.ByKind = "mirror" }))
	projection, err := s.Replay()
	if err != nil || projection.Latest("nife") == nil || projection.Latest("impl-nife") == nil {
		t.Fatalf("post-window mirror was merged: err=%v names=%v", err, projection.Names())
	}
}

func TestAliasPreservesExplicitAssignment(t *testing.T) {
	state, s := scratch(t)
	fixture, err := os.ReadFile("testdata/live-alias-nife.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.EventsPath(), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	assignmentAt := time.Date(2026, 9, 10, 5, 28, 0, 0, time.UTC)
	mustAppend(t, s, Event{ID: NewID(assignmentAt), At: assignmentAt, Kind: KindAssign, By: "ziru", ByKind: "agent", Name: "impl-nife", Manager: "vara"})
	projection, err := s.Replay()
	if err != nil || projection.Latest("impl-nife").Manager != "vara" {
		t.Fatalf("assignment lost during alias repair: err=%v view=%+v", err, projection.Latest("impl-nife"))
	}
}

func TestAliasManagerRepairDoesNotOverrideAgentLauncher(t *testing.T) {
	_, s := scratch(t)
	req := ev(KindLaunchRequested, "", 1, func(e *Event) {
		e.Tool, e.Tag, e.By = "codex", "impl", "impl-pimi"
		e.Placement = &Placement{Workspace: "w80"}
	})
	mustAppend(t, s, req)
	mustAppend(t, s, ev(KindLaunchReady, "impl-x", 2, func(e *Event) { e.Request, e.By = req.ID, "impl-pimi" }))
	mustAppend(t, s, ev(KindMirrorReady, "x", 3, func(e *Event) { e.By, e.ByKind = "pimi", "mirror" }))
	projection, err := s.Replay()
	if err != nil || projection.Latest("impl-x").Manager != "impl-pimi" {
		t.Fatalf("agent launcher manager overwritten: err=%v view=%+v", err, projection.Latest("impl-x"))
	}
}

func TestOldProjectionVersionForcesReplay(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindAnnotate, "real", 1, func(e *Event) { e.Title = "from journal" }))
	stat, err := os.Stat(s.EventsPath())
	if err != nil {
		t.Fatal(err)
	}
	// Pin the immediately previous version so every fold change requires a bump.
	raw := fmt.Sprintf(`{"version":%d,"events_offset":%d,"agents":{"stale":[]},"requests":{},"unnamed_sessions":{}}`, ProjectionVersion-1, stat.Size())
	if err := os.WriteFile(s.SnapshotPath(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	projection, err := s.LoadNoSnapshot()
	if err != nil || projection.Latest("real") == nil || projection.Latest("stale") != nil {
		t.Fatalf("old snapshot was trusted: err=%v names=%v", err, projection.Names())
	}
}

func TestAssignmentGroupUsesEventTimeOrder(t *testing.T) {
	projection := NewProjection()
	projection.Apply(ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Group = "new" }), 0)
	projection.Apply(ev(KindAssign, "impl-gime", 1, func(e *Event) { e.ClearGroup = true }), 0)
	view := projection.Latest("impl-gime")
	if view == nil || view.Assignment == nil || view.Assignment.Group != "new" || view.AssignmentAt == nil || !view.AssignmentAt.Equal(at(2)) {
		t.Fatalf("backdated clear changed assignment: %+v", view)
	}
	projection.Apply(ev(KindAssign, "impl-gime", 3, func(e *Event) { e.ClearGroup = true }), 0)
	view = projection.Latest("impl-gime")
	if view.Assignment != nil || view.AssignmentAt == nil || !view.AssignmentAt.Equal(at(3)) {
		t.Fatalf("later clear did not win: %+v", view)
	}
	projection.Apply(ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Group = "stale" }), 0)
	if view = projection.Latest("impl-gime"); view.Assignment != nil {
		t.Fatalf("backdated group revived cleared assignment: %+v", view)
	}
}

func TestAnnotateFoldsFieldWise(t *testing.T) {
	for _, order := range []string{"note-title", "title-note"} {
		t.Run(order, func(t *testing.T) {
			_, s := scratch(t)
			first, second := ev(KindAnnotate, "a", 1, func(e *Event) { e.Note = "kept note" }), ev(KindAnnotate, "a", 2, func(e *Event) { e.Title = "kept title"; e.By = "latest" })
			if order == "title-note" {
				first, second = ev(KindAnnotate, "a", 1, func(e *Event) { e.Title = "kept title" }), ev(KindAnnotate, "a", 2, func(e *Event) { e.Note = "kept note"; e.By = "latest" })
			}
			mustAppend(t, s, first)
			mustAppend(t, s, second)
			view, _ := s.Replay()
			annotation := view.Latest("a").Annotation
			if annotation.Title != "kept title" || annotation.Note != "kept note" || annotation.By != "latest" || !annotation.At.Equal(second.At) {
				t.Fatalf("annotation = %+v", annotation)
			}
		})
	}
}

func TestAnnotateTitleValidation(t *testing.T) {
	for name, title := range map[string]string{
		"control character":  "bad\tname",
		"more than 80 runes": strings.Repeat("界", 81),
	} {
		t.Run(name, func(t *testing.T) {
			_, s := scratch(t)
			if _, err := s.Append(ev(KindAnnotate, "a", 1, func(e *Event) { e.Title = title })); err == nil {
				t.Fatal("invalid title accepted")
			}
		})
	}
}

func TestReusedNameIsANewIncarnationThatInheritsNothing(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 1, func(e *Event) { e.Pane = "w80:p1"; e.Tool = "codex" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Group = "old-mission" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 3, func(e *Event) { e.Manager = "old-manager" }))
	mustAppend(t, s, ev(KindCulled, "impl-gime", 4, func(e *Event) { e.Pane, e.Close = "w80:p1", "managed" }))
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 10, func(e *Event) { e.By = "vara"; e.Pane = "w81:p1" }))
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	old := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(0)})
	fresh := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(9)})
	latest := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime"})
	if old == nil || old.Assignment == nil || old.Assignment.Group != "old-mission" || old.Manager != "old-manager" || old.Closed == nil {
		t.Fatalf("old incarnation = %+v", old)
	}
	if fresh == nil || fresh.Assignment != nil || fresh.Manager != "vara" || fresh.Provenance.Launcher != "vara" || fresh.Closed != nil || !fresh.Incarnation.Equal(at(9)) {
		t.Fatalf("fresh incarnation inherited history: %+v", fresh)
	}
	if latest == nil || latest.Provenance.Launcher != "vara" || !latest.Incarnation.Equal(at(10)) {
		t.Fatalf("no roster time should pick the latest: %+v", latest)
	}
	if v := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(4)}); v != nil {
		t.Fatalf("created after the old record's first event must not inherit it: %+v", v)
	}
	if v := proj.View("impl-gime", &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(5)}); v == nil || v.Manager != "vara" {
		t.Fatalf("created after the cull must map to the new incarnation: %+v", v)
	}
}

func TestResumeKeepsManagerGroupAndTitleInTheSameIncarnation(t *testing.T) {
	_, store := scratch(t)
	mustAppend(t, store, ev(KindLaunchReady, "impl-gime", 1, func(e *Event) { e.Session = "session-a" }))
	mustAppend(t, store, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Manager, e.Group = "ziru", "fleet-refit" }))
	mustAppend(t, store, ev(KindAnnotate, "impl-gime", 3, func(e *Event) { e.Title = "payload builder" }))
	mustAppend(t, store, ev(KindResume, "impl-gime", 4, func(e *Event) { e.FromSession, e.Pane = "session-a", "w80:p2" }))
	projection, err := store.Replay()
	if err != nil {
		t.Fatal(err)
	}
	view := projection.Latest("impl-gime")
	if view == nil || view.Manager != "ziru" || view.Assignment == nil || view.Assignment.Group != "fleet-refit" || view.Annotation == nil || view.Annotation.Title != "payload builder" || view.Provenance.Pane != "w80:p2" {
		t.Fatalf("resume dropped supervision metadata: %+v", view)
	}
}

func TestReusedNameWithoutACloseEventInheritsNothing(t *testing.T) {
	// Raw `hcom kill`, a crash or a missed wrapper leaves no culled event.
	// Roster creation after the record's first event is the newer evidence.
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 1, func(e *Event) { e.Session = "old-S" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Group = "old-mission" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 3, func(e *Event) { e.Manager = "old-manager" }))
	proj, _ := s.Replay()
	later := &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(10), SessionID: "new-S"}
	if v := proj.View("impl-gime", later); v != nil {
		t.Fatalf("reused name without a close event inherited manager %q mission %+v", v.Manager, v.Assignment)
	}
	same := &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(10), SessionID: "old-S"}
	if v := proj.View("impl-gime", same); v == nil || v.Manager != "old-manager" {
		t.Fatalf("a matching open session proves the same incarnation across a late created_at: %+v", v)
	}
	before := &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(0), SessionID: "new-S"}
	if v := proj.View("impl-gime", before); v == nil || v.Assignment == nil {
		t.Fatalf("created before the first event is the same life: %+v", v)
	}
}

func TestSameIDWithDifferentPayloadIsRejectedNotReplayed(t *testing.T) {
	state, s := scratch(t)
	e := ev(KindAssign, "impl-lima", 1, func(e *Event) { e.Group = "fleet-refit" })
	mustAppend(t, s, e)
	changed := e
	changed.Group = "corrected"
	_, err := s.Append(changed)
	if err == nil || errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "already has a different payload") {
		t.Fatalf("err = %v", err)
	}
	if n := len(lines(t, s.EventsPath())); n != 1 {
		t.Fatalf("lines = %d, want 1", n)
	}
	// CLI: exit 2, no line, original retained.
	cmd := exec.Command(herderBin, "register", "assign", "--name", "impl-lima", "--group", "corrected", "--id", e.ID)
	cmd.Env = append(os.Environ(), "HERDER_STATE_DIR="+state)
	out, cliErr := cmd.CombinedOutput()
	exit, ok := cliErr.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || !strings.Contains(string(out), "different payload") {
		t.Fatalf("cli: err=%v out=%s", cliErr, out)
	}
	proj, _ := s.Replay()
	if proj.Latest("impl-lima").Assignment.Group != "fleet-refit" || len(lines(t, s.EventsPath())) != 1 {
		t.Fatal("conflicting payload changed the store")
	}
}

func TestPackageAppendEnforcesTheCLIContract(t *testing.T) {
	_, s := scratch(t)
	steer := -1
	for name, bad := range map[string]Event{
		"unsupported tool":          ev(KindLaunchRequested, "", 1, func(e *Event) { e.Tool, e.Tag = "gemini", "t"; e.Placement = &Placement{Workspace: "w"} }),
		"missing launch tag":        ev(KindLaunchRequested, "", 1, func(e *Event) { e.Tool = "claude"; e.Placement = &Placement{Workspace: "w"} }),
		"two placement targets":     ev(KindLaunchRequested, "", 1, func(e *Event) { e.Tool, e.Tag = "claude", "t"; e.Placement = &Placement{Workspace: "w", Pane: "p"} }),
		"culled without pane":       ev(KindCulled, "a", 1, func(e *Event) { e.Close = "managed" }),
		"annotate with manager":     ev(KindAnnotate, "a", 1, func(e *Event) { e.Title, e.Manager = "t", "m" }),
		"invalid by_kind":           ev(KindAssign, "a", 1, func(e *Event) { e.Group, e.ByKind = "m", "browser" }),
		"empty assignment":          ev(KindAssign, "a", 1),
		"group and clear":           ev(KindAssign, "a", 1, func(e *Event) { e.Group, e.ClearGroup = "m", true }),
		"long group":                ev(KindAssign, "a", 1, func(e *Event) { e.Group = strings.Repeat("界", 81) }),
		"negative steer_chars":      ev(KindCompactRequested, "a", 1, func(e *Event) { e.SteerChars = &steer }),
		"top-level pane on request": ev(KindLaunchRequested, "", 1, func(e *Event) { e.Tool, e.Tag, e.Pane = "claude", "t", "p" }),
	} {
		if _, err := s.Append(bad); err == nil || errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: accepted (err=%v)", name, err)
		}
	}
	if _, statErr := os.Stat(s.EventsPath()); statErr == nil {
		t.Fatal("an invalid event reached the journal")
	}
	zero := 0
	for name, good := range map[string]Event{
		"batch without name":  ev(KindMirrorBatch, "", 1, func(e *Event) { e.Instances = []string{"a", "b"} }),
		"pane-only session":   ev(KindSessionObserved, "", 1, func(e *Event) { e.Session, e.Tool = "s", "claude" }),
		"zero steer_chars":    ev(KindCompactRequested, "a", 1, func(e *Event) { e.SteerChars = &zero }),
		"ready with launcher": ev(KindLaunchReady, "a", 1, func(e *Event) { e.Tool, e.LauncherKind = "codex", "web"; e.Placement = &Placement{Workspace: "w"} }),
		"worktree placement": ev(KindLaunchRequested, "", 1, func(e *Event) {
			e.Tool, e.Tag = "codex", "t"
			e.Placement = &Placement{WorktreeBranch: "topic", Repo: "/repo"}
		}),
	} {
		if _, err := s.Append(good); err != nil {
			t.Errorf("%s: rejected: %v", name, err)
		}
	}
}

func TestInterruptedImportLeavesNoJournalAndReopenImportsEverything(t *testing.T) {
	state := t.TempDir()
	var edges bytes.Buffer
	const total = 1000
	for i := 0; i < total; i++ {
		fmt.Fprintf(&edges, `{"name":"edge-%04d","launcher":"web-yamen","tool":"claude","tag":"t","workspace":"w1","pane":"w1:p%d","time":"2026-09-08T22:21:%02dZ"}`+"\n", i, i, i%60)
	}
	if err := os.WriteFile(filepath.Join(state, "launch-edges.jsonl"), edges.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	faulty := &Store{Dir: filepath.Join(state, "agents"), EdgesPath: filepath.Join(state, "launch-edges.jsonl"), Stderr: &stderr}
	faulty.importFault = func(n int) error {
		if n == 4 {
			return errors.New("simulated crash after 4 edges")
		}
		return nil
	}
	if err := faulty.importEdges(); err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("faulty import err = %v", err)
	}
	if _, err := os.Stat(faulty.EventsPath()); err == nil {
		t.Fatal("a partial events.jsonl was published")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(state, "agents", ".import-*")); len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
	s := Open(state, &stderr)
	if s.ImportErr != nil {
		t.Fatal(s.ImportErr)
	}
	if n := len(lines(t, s.EventsPath())); n != total {
		t.Fatalf("lines after reopen = %d, want %d", n, total)
	}
	proj, _ := s.Replay()
	if len(proj.Agents) != total || proj.Latest("edge-0999").Provenance.LauncherKind != "web" {
		t.Fatalf("agents = %d", len(proj.Agents))
	}
	// A concurrent opener blocked on init.lock sees the finished import, not a second one.
	Open(state, &stderr)
	if n := len(lines(t, s.EventsPath())); n != total {
		t.Fatalf("second open changed the journal: %d", n)
	}
}

func TestMirrorBatchFansOutToInstances(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindMirrorBatch, "", 1, func(e *Event) {
		e.By, e.ByKind = "ziru", "mirror"
		e.Instances = []string{"mavu", "vile"}
		e.Batch = "b1"
	}))
	proj, _ := s.Replay()
	for _, name := range []string{"mavu", "vile"} {
		v := proj.Latest(name)
		if v == nil || v.Provenance.Kind != "mirrored" || v.Provenance.Launcher != "ziru" || v.Provenance.LauncherKind != "mirror" {
			t.Fatalf("%s = %+v", name, v)
		}
	}
	if _, named := proj.Agents[""]; named {
		t.Fatal("an empty name was recorded")
	}
	// Pane-only session: no name, keyed by tool/session.
	mustAppend(t, s, ev(KindSessionObserved, "", 2, func(e *Event) { e.Session, e.Tool, e.Path = "s-9", "claude", "/p" }))
	mustAppend(t, s, ev(KindSessionEnded, "", 3, func(e *Event) { e.Session, e.Tool = "s-9", "claude" }))
	proj, _ = s.Replay()
	if u := proj.UnnamedSessions["claude/s-9"]; u == nil || u.Path != "/p" || u.Ended == nil {
		t.Fatalf("unnamed session = %+v", u)
	}
}

func TestSessionEndedClosesTheNamedSessionOnly(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "a", 1, func(e *Event) { e.Session = "S" }))
	mustAppend(t, s, ev(KindSessionObserved, "a", 2, func(e *Event) { e.Session = "S-new" }))
	mustAppend(t, s, ev(KindSessionEnded, "a", 3, func(e *Event) { e.Session = "S"; e.Reason = "exit:other" }))
	proj, _ := s.Replay()
	v := proj.View("a", nil)
	if v.Session == nil || v.Session.SessionID != "S-new" || v.Sessions[0].Ended != nil {
		t.Fatalf("session.ended S closed the current session: %+v", v.Sessions)
	}
	if v.Sessions[1].SessionID != "S" || v.Sessions[1].Ended == nil || v.Sessions[1].EndReason != "superseded" {
		t.Fatalf("old session = %+v", v.Sessions[1])
	}
}

func TestOlderAssignmentArrivingLateDoesNotOverwriteNewerManager(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "a", 1))
	mustAppend(t, s, ev(KindAssign, "a", 5, func(e *Event) { e.Manager = "newer" }))
	mustAppend(t, s, ev(KindAssign, "a", 3, func(e *Event) { e.Manager = "older-arrived-late" }))
	proj, _ := s.Replay()
	if v := proj.Latest("a"); v.Manager != "newer" || v.EventCount != 3 {
		t.Fatalf("manager = %q events %d", v.Manager, v.EventCount)
	}
}

func TestRegisteredReadySupersedesMirrorAttribution(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindMirrorCreated, "a", 1, func(e *Event) { e.By, e.ByKind = "unknown", "mirror" }))
	mustAppend(t, s, ev(KindLaunchReady, "a", 2, func(e *Event) { e.By, e.ByKind = "web-owner", "web"; e.LauncherKind = "web" }))
	proj, _ := s.Replay()
	v := proj.Latest("a")
	if v.Provenance.Kind != "registered" || v.Provenance.Launcher != "web-owner" || v.Provenance.LauncherKind != "web" || v.Manager != "web-owner" {
		t.Fatalf("view = %+v", v.Provenance)
	}
	// An explicit assignment before the registered ready is kept.
	_, s2 := scratch(t)
	mustAppend(t, s2, ev(KindMirrorCreated, "b", 1, func(e *Event) { e.By, e.ByKind = "unknown", "mirror" }))
	mustAppend(t, s2, ev(KindAssign, "b", 2, func(e *Event) { e.Manager = "vara" }))
	mustAppend(t, s2, ev(KindLaunchReady, "b", 3, func(e *Event) { e.By = "ziru" }))
	proj, _ = s2.Replay()
	if v := proj.Latest("b"); v.Provenance.Launcher != "ziru" || v.Manager != "vara" {
		t.Fatalf("explicit assignment lost: launcher=%q manager=%q", v.Provenance.Launcher, v.Manager)
	}
	// A later mirror never demotes a registered launcher.
	mustAppend(t, s, ev(KindMirrorReady, "a", 3, func(e *Event) { e.By, e.ByKind = "user", "mirror" }))
	proj, _ = s.Replay()
	if v := proj.Latest("a"); v.Provenance.Launcher != "web-owner" {
		t.Fatalf("mirror demoted registered launcher: %+v", v.Provenance)
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

func TestAssignmentChangesManagerNotLauncher(t *testing.T) {
	_, s := scratch(t)
	req := ev(KindLaunchRequested, "", 1, func(e *Event) { e.Tool, e.Tag = "claude", "impl"; e.Placement = &Placement{Pane: "w1:p1"} })
	mustAppend(t, s, req)
	mustAppend(t, s, ev(KindLaunchReady, "impl-lima", 2, func(e *Event) { e.Request = req.ID; e.By = "spawn-wrapper" }))
	proj, _ := s.Replay()
	v := proj.Latest("impl-lima")
	if v.Provenance.Launcher != "ziru" || v.Manager != "ziru" || v.Provenance.PaneRequested != "w1:p1" {
		t.Fatalf("before assignment: %+v", v)
	}
	mustAppend(t, s, ev(KindAssign, "impl-lima", 3, func(e *Event) { e.Manager, e.By = "vara", "bigboss" }))
	proj, _ = s.Replay()
	v = proj.Latest("impl-lima")
	if v.Provenance.Launcher != "ziru" || v.Manager != "vara" || v.ManagerBy != "bigboss" {
		t.Fatalf("after assignment: launcher=%q manager=%q by=%q", v.Provenance.Launcher, v.Manager, v.ManagerBy)
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
	mustAppend(t, s, ev(KindAssign, "a", 1, func(e *Event) { e.Group = "m" }))
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(state, "agents")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	proj, err := s.Load()
	if err != nil || proj.SnapshotErr == nil || proj.Latest("a") == nil || proj.Latest("a").Assignment.Group != "m" {
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
