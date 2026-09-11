package agentstore

import (
	"bytes"
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
