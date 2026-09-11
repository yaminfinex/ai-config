package agentstore

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
)

// ruzuJournal is the query-topo-guna shape from diag-ruzu (journal 239): a
// life closed by mirror.stopped, an annotate that opens a new record BEFORE
// hcom creates the roster row, then the roster row's mirror.ready.
func ruzuJournal(t *testing.T, readySession string) *Store {
	t.Helper()
	_, s := scratch(t)
	mustAppend(t, s, ev(KindMirrorReady, "query-topo-guna", 1, func(e *Event) { e.ByKind = "mirror"; e.Session = "old-S" }))
	mustAppend(t, s, ev(KindAssign, "query-topo-guna", 2, func(e *Event) { e.Manager, e.Group = "old-manager", "old-mission" }))
	mustAppend(t, s, ev(KindMirrorStopped, "query-topo-guna", 3, func(e *Event) { e.ByKind, e.Reason = "mirror", "exit:other" }))
	mustAppend(t, s, ev(KindAnnotate, "query-topo-guna", 10, func(e *Event) { e.Title = "queries-program-lead" }))
	mustAppend(t, s, ev(KindMirrorReady, "query-topo-guna", 12, func(e *Event) { e.ByKind = "mirror"; e.Session = readySession }))
	return s
}

func TestMirrorReadyWithSessionBindsARecordOpenedBeforeTheRosterRow(t *testing.T) {
	s := ruzuJournal(t, "new-S")
	mustAppend(t, s, ev(KindAssign, "query-topo-guna", 13, func(e *Event) { e.Manager, e.Group = "ziru", "fleet-refit" }))
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(proj.Agents["query-topo-guna"]); n != 2 {
		t.Fatalf("incarnations = %d, want 2", n)
	}
	// roster created_at (11) is AFTER the record's FirstSeen (10): only the
	// session evidence proves the record is this life.
	got := proj.Incarnation("query-topo-guna", at(11), "new-S")
	if got == nil || got.Manager != "ziru" || got.Assignment == nil || got.Assignment.Group != "fleet-refit" || currentSession(got) != "new-S" {
		t.Fatalf("mirror.ready session did not bind the record: %+v", got)
	}
	if v := proj.View("query-topo-guna", &hcomidentity.Row{Name: "query-topo-guna", CreatedAt: at(11), SessionID: "new-S"}); v == nil || v.Manager != "ziru" {
		t.Fatalf("View = %+v", v)
	}
	if v := proj.Incarnation("query-topo-guna", at(11), "other-S"); v != nil {
		t.Fatalf("a different roster session must still be refused (name reuse): %+v", v)
	}
}

func TestMirrorReadyWithoutSessionStillRefusesTheEarlierRecord(t *testing.T) {
	s := ruzuJournal(t, "")
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if v := proj.Incarnation("query-topo-guna", at(11), "new-S"); v != nil {
		t.Fatalf("session-less record leaked across a later created_at: %+v", v)
	}
	if v := proj.View("query-topo-guna", &hcomidentity.Row{Name: "query-topo-guna", CreatedAt: at(11), SessionID: "new-S"}); v != nil {
		t.Fatalf("View must be nil (unregistered) without session evidence: %+v", v)
	}
}

func TestResumeWithSessionBindsTheNewSession(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 1, func(e *Event) { e.Session = "session-a"; e.Tool = "claude" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Manager, e.Group = "ziru", "fleet-refit" }))
	mustAppend(t, s, ev(KindResume, "impl-gime", 4, func(e *Event) { e.FromSession, e.Session, e.Pane = "session-a", "session-b", "w80:p2" }))
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	view := proj.Latest("impl-gime")
	if view == nil || currentSession(view) != "session-b" || len(view.Sessions) != 2 || view.Sessions[1].Ended == nil || view.Sessions[0].Tool != "claude" {
		t.Fatalf("resume did not bind session-b: %+v", view)
	}
	if v := proj.Incarnation("impl-gime", at(3), "session-b"); v == nil || v.Manager != "ziru" {
		t.Fatalf("resumed session is the same incarnation: %+v", v)
	}
	if v := proj.Incarnation("impl-gime", at(3), "session-a"); v != nil {
		t.Fatalf("the ended session no longer proves the record: %+v", v)
	}
}

func TestSnapshotPlusTailEqualsFullReplayForTheRuzuJournal(t *testing.T) {
	s := ruzuJournal(t, "new-S")
	snap, err := s.Load() // snapshot after the stamped mirror.ready
	if err != nil || snap.SnapshotErr != nil || snap.Version != ProjectionVersion {
		t.Fatalf("load: %v snapshotErr %v version %d", err, snap.SnapshotErr, snap.Version)
	}
	s.replays.Store(0)
	mustAppend(t, s, ev(KindAssign, "query-topo-guna", 13, func(e *Event) { e.Manager, e.Group = "ziru", "fleet-refit" }))
	mustAppend(t, s, ev(KindResume, "query-topo-guna", 14, func(e *Event) { e.FromSession, e.Session = "new-S", "new-S2" }))
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
	if v := tail.Incarnation("query-topo-guna", at(11), "new-S2"); v == nil || v.Manager != "ziru" {
		t.Fatalf("tail view = %+v", v)
	}
}

// vipe D1: a reused name over an UNCLOSED life. The old record has session
// history (OLD, never ended); the roster row for the new life carries NEW
// and its mirror.ready is stamped NEW. The mirror must not bind NEW onto the
// old record, so the new roster still folds as unregistered and the old
// manager/group never appear.
func TestMirrorReadyDoesNotBindOverAnUnclosedLifeWithSessionHistory(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 1, func(e *Event) { e.Session = "OLD" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Manager, e.Group = "old-manager", "old-group" }))
	mustAppend(t, s, ev(KindMirrorReady, "impl-gime", 12, func(e *Event) { e.ByKind = "mirror"; e.Session = "NEW" }))
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	old := proj.Latest("impl-gime")
	if len(proj.Agents["impl-gime"]) != 1 || currentSession(old) != "OLD" || len(old.Sessions) != 1 {
		t.Fatalf("mirror touched a record with session history: %+v", old.Sessions)
	}
	row := &hcomidentity.Row{Name: "impl-gime", CreatedAt: at(11), SessionID: "NEW"}
	if v := proj.Incarnation("impl-gime", at(11), "NEW"); v != nil {
		t.Fatalf("NEW bound over an unclosed life: manager %q", v.Manager)
	}
	if v := proj.View("impl-gime", row); v != nil {
		t.Fatalf("old-manager/old-group leaked into the new roster row: %+v", v)
	}
	// the same open session mirrored again is a no-op
	mustAppend(t, s, ev(KindMirrorReady, "impl-gime", 13, func(e *Event) { e.ByKind = "mirror"; e.Session = "OLD" }))
	proj, _ = s.Replay()
	if v := proj.Latest("impl-gime"); len(v.Sessions) != 1 || v.Sessions[0].Ended != nil {
		t.Fatalf("same-session mirror changed history: %+v", v.Sessions)
	}
}

// vipe D3: claude keeps its session id across --resume, so a resume naming the
// open session is a no-op on Sessions; a resume to a different id ends the old
// one "resumed" and opens the new one.
func TestResumeWithTheSameSessionKeepsItOpen(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindLaunchReady, "impl-gime", 1, func(e *Event) { e.Session = "S"; e.Tool = "claude" }))
	mustAppend(t, s, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Manager = "ziru" }))
	mustAppend(t, s, ev(KindResume, "impl-gime", 4, func(e *Event) { e.FromSession, e.Session, e.Pane = "S", "S", "w80:p2" }))
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	v := proj.Latest("impl-gime")
	if len(v.Sessions) != 1 || v.Sessions[0].Ended != nil || currentSession(v) != "S" || v.Provenance.Pane != "w80:p2" {
		t.Fatalf("same-id resume changed sessions: %+v", v)
	}
	if b := proj.Incarnation("impl-gime", at(3), "S"); b == nil || b.Manager != "ziru" {
		t.Fatalf("binding lost after same-id resume: %+v", b)
	}
	mustAppend(t, s, ev(KindResume, "impl-gime", 5, func(e *Event) { e.FromSession, e.Session = "S", "T" }))
	proj, _ = s.Replay()
	v = proj.Latest("impl-gime")
	if len(v.Sessions) != 2 || currentSession(v) != "T" || v.Sessions[1].SessionID != "S" || v.Sessions[1].Ended == nil || v.Sessions[1].EndReason != "resumed" {
		t.Fatalf("S->T resume: %+v", v.Sessions)
	}
}

// vipe D4: a LITERAL version-6 snapshot (the fold that ignored mirror
// sessions) at the journal's current offset. Load must reject it by version,
// replay, and the record must carry the mirror.ready session. Reverting
// ProjectionVersion to 6 reds this test.
func TestLiteralVersion6SnapshotIsReplacedAndMirrorSessionReplayed(t *testing.T) {
	_, s := scratch(t)
	mustAppend(t, s, ev(KindMirrorReady, "query-topo-guna", 12, func(e *Event) { e.ByKind = "mirror"; e.Session = "new-S" }))
	stat, err := os.Stat(s.EventsPath())
	if err != nil {
		t.Fatal(err)
	}
	v6 := fmt.Sprintf(`{"version":6,"events_offset":%d,"agents":{"query-topo-guna":[{"name":"query-topo-guna","incarnation":"2026-09-09T05:00:12Z","first_seen":"2026-09-09T05:00:12Z","last_seen":"2026-09-09T05:00:12Z","provenance":{"kind":"mirrored","state":"ready"},"events":[]}]},"requests":{},"unnamed_sessions":{}}`, stat.Size())
	if err := os.WriteFile(s.SnapshotPath(), []byte(v6), 0o600); err != nil {
		t.Fatal(err)
	}
	proj, err := s.Load()
	if err != nil || proj.SnapshotErr != nil {
		t.Fatalf("load: %v snapshotErr %v", err, proj.SnapshotErr)
	}
	v := proj.Latest("query-topo-guna")
	if proj.Version != 7 || v == nil || currentSession(v) != "new-S" {
		t.Fatalf("v6 snapshot trusted: version=%d view=%+v", proj.Version, v)
	}
	if b := proj.Incarnation("query-topo-guna", at(11), "new-S"); b == nil {
		t.Fatal("replayed record does not bind the roster session")
	}
}
