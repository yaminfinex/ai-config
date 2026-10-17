package agentstore

import (
	"bytes"
	"os"
	"reflect"
	"strconv"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
)

// laneID is a stable lane id for a test label.
func laneID(label string) string { return DerivedID([]byte("lane:" + label)) }

func laneCreated(name, lead string, sec int) Event {
	return ev(KindLaneCreated, "", sec, func(e *Event) { e.LaneName, e.Lead = name, lead })
}

func TestLaneFoldKeepsEarlierNamesLeadAndLinks(t *testing.T) {
	p := NewProjection()
	created := laneCreated("signup", "lead-a", 1)
	id := created.ID
	for _, e := range []Event{
		created,
		ev(KindLaneUpdated, "", 2, func(e *Event) { e.Lane, e.LaneName = id, "turnkey-signup" }),
		ev(KindLaneUpdated, "", 3, func(e *Event) { e.Lane, e.LaneName, e.Lead = id, "turnkey", "lead-b" }),
		ev(KindLaneUpdated, "", 2, func(e *Event) { e.Lane, e.Lead = id, "stale" }), // backdated lead loses
		ev(KindLaneLink, "", 4, func(e *Event) { e.Lane, e.PR = id, "o/r#12" }),
		ev(KindLaneLink, "", 4, func(e *Event) { e.Lane, e.PR = id, "o/r#13" }),
		ev(KindLaneLink, "", 5, func(e *Event) { e.Lane, e.PR = id, "o/r#12" }), // duplicate link
		ev(KindLaneLink, "", 6, func(e *Event) { e.Lane, e.PR, e.Remove = id, "o/r#12", true }),
		ev(KindLaneLink, "", 7, func(e *Event) { e.Lane, e.Repo = id, "o/other" }),
		ev(KindLaneUpdated, "", 8, func(e *Event) { e.Lane, e.LaneName = laneID("unknown"), "ghost" }),
	} {
		p.Apply(e, 0)
	}
	lane := p.Lanes[id]
	want := &LaneView{ID: id, Name: "turnkey", Names: []string{"signup", "turnkey-signup"}, Lead: "lead-b", LeadAt: at(3), Created: at(1), By: "ziru", PRs: []string{"o/r#13"}, Repos: []string{"o/other"}}
	if !reflect.DeepEqual(lane, want) {
		t.Fatalf("lane = %+v\nwant %+v", lane, want)
	}
	if len(p.Lanes) != 1 {
		t.Fatalf("an update for an unknown lane invented one: %+v", p.Lanes)
	}
	if p.Latest("") != nil {
		t.Fatal("a name-less lane event opened an agent record")
	}
}

func TestLaneMembersFollowTheRosterIncarnation(t *testing.T) {
	_, s := scratch(t)
	created := mustAppend(t, s, laneCreated("refit", "impl-gime", 1)).Event
	mustAppend(t, s, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Lane = created.ID }))
	mustAppend(t, s, ev(KindAssign, "impl-lima", 2, func(e *Event) { e.Lane = created.ID }))
	mustAppend(t, s, ev(KindAssign, "impl-lima", 3, func(e *Event) { e.Manager = "ziru" })) // manager-only keeps the lane
	mustAppend(t, s, ev(KindAssign, "impl-vara", 2, func(e *Event) { e.Lane = created.ID }))
	mustAppend(t, s, ev(KindCulled, "impl-vara", 4, func(e *Event) { e.Pane, e.Close = "p", "managed" }))
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	roster := []hcomidentity.Row{
		{Name: "impl-gime", CreatedAt: at(0)},
		{Name: "impl-lima", CreatedAt: at(0)},
		{Name: "impl-vara", CreatedAt: at(5)}, // a new life after the cull
	}
	roster[0].BaseName = "gime"
	roster = append(roster, hcomidentity.Row{Name: "gime_explore_1", ParentName: "gime", CreatedAt: at(6)}) // a Task child follows its owner
	got := proj.LaneMembers(roster)
	if want := map[string][]string{created.ID: {"impl-gime", "impl-lima", "gime_explore_1"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("members = %v, want %v", got, want)
	}
}

// Old group lines are history the move script reads; they still fold.
func TestRetiredGroupLinesStillFold(t *testing.T) {
	_, s := scratch(t)
	line := `{"id":"` + NewID(at(1)) + `","at":"2026-09-09T05:00:01Z","kind":"assign","by":"ziru","name":"impl-gime","group":"fleet-refit"}` + "\n"
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.EventsPath(), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	proj, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if v := proj.Latest("impl-gime"); v == nil || v.Assignment == nil || v.Assignment.Group != "fleet-refit" {
		t.Fatalf("old group line did not fold: %+v", v)
	}
	if _, err := s.Append(ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Group = "x" })); err == nil {
		t.Fatal("a new group line was accepted")
	}
}

func TestLaneSnapshotPlusTailEqualsFullReplay(t *testing.T) {
	_, s := scratch(t)
	created := mustAppend(t, s, laneCreated("refit", "impl-gime", 1)).Event
	mustAppend(t, s, ev(KindAssign, "impl-gime", 2, func(e *Event) { e.Lane = created.ID }))
	if snap, err := s.Load(); err != nil || snap.SnapshotErr != nil {
		t.Fatalf("load: %v", err)
	}
	s.replays.Store(0)
	mustAppend(t, s, ev(KindLaneUpdated, "", 3, func(e *Event) { e.Lane, e.LaneName, e.Lead = created.ID, "refit-2", "impl-lima" }))
	mustAppend(t, s, ev(KindLaneLink, "", 4, func(e *Event) { e.Lane, e.PR = created.ID, "o/r#7" }))
	mustAppend(t, s, ev(KindAssign, "impl-lima", 5, func(e *Event) { e.Lane = created.ID }))
	second := mustAppend(t, s, laneCreated("other", "impl-gime", 6)).Event
	mustAppend(t, s, ev(KindAssign, "impl-gime", 7, func(e *Event) { e.Lane = second.ID }))
	tail, err := s.LoadNoSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if s.replays.Load() != 0 {
		t.Fatal("clean snapshot tail triggered a full replay")
	}
	full, err := s.Replay()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := tail.Marshal()
	b, _ := full.Marshal()
	if !bytes.Equal(a, b) {
		t.Fatalf("snapshot+tail != replay\n%s\n%s", a, b)
	}
	if full.Lanes[created.ID].Name != "refit-2" || len(full.Lanes) != 2 {
		t.Fatalf("lanes = %+v", full.Lanes)
	}
}

func TestVersion7SnapshotIsReplayed(t *testing.T) {
	_, s := scratch(t)
	created := mustAppend(t, s, laneCreated("refit", "impl-gime", 1)).Event
	info, err := os.Stat(s.EventsPath())
	if err != nil {
		t.Fatal(err)
	}
	// A v7 snapshot that already covers the lane line but knows no lanes.
	stale := []byte(`{"version":7,"events_offset":` + strconv.FormatInt(info.Size(), 10) + `,"agents":{},"requests":{},"unnamed_sessions":{}}`)
	if err := os.WriteFile(s.SnapshotPath(), stale, 0o600); err != nil {
		t.Fatal(err)
	}
	proj, err := s.LoadNoSnapshot()
	if err != nil || proj.Lanes[created.ID] == nil {
		t.Fatalf("v7 snapshot trusted: err=%v lanes=%v", err, proj.Lanes)
	}
}
