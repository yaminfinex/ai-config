package hcomevents

import "testing"

func TestTurnEndsCountsOnlyCompletedTurns(t *testing.T) {
	ends := NewTurnEnds()
	ends.Apply(Status{ID: 10, Instance: "mole", OldStatus: "pending", NewStatus: "listening", Session: "s1"})
	if id, ok := ends.Lookup("mole", "s1"); ok {
		t.Fatalf("launch counted as a turn end: %d", id)
	}
	ends.Apply(Status{ID: 12, Instance: "mole", OldStatus: "active", NewStatus: "listening", Session: "s1"})
	if id, ok := ends.Lookup("mole", "s1"); !ok || id != 12 {
		t.Fatalf("active->listening = %d %v, want 12", id, ok)
	}
	ends.Apply(Status{ID: 14, Instance: "mole", OldStatus: "listening", NewStatus: "listening", Session: "s1"})
	ends.Apply(Status{ID: 15, Instance: "mole", OldStatus: "active", NewStatus: "blocked", Session: "s1"})
	ends.Apply(Status{ID: 16, Instance: "mole", OldStatus: "inactive", NewStatus: "listening", Session: "s1"})
	if id, _ := ends.Lookup("mole", "s1"); id != 12 {
		t.Fatalf("listening->listening, non-listening entry or reattach moved the turn: %d", id)
	}
	ends.Apply(Status{ID: 18, Instance: "mole", OldStatus: "blocked", NewStatus: "listening", Session: "s1"})
	if id, _ := ends.Lookup("mole", "s1"); id != 18 {
		t.Fatalf("blocked->listening = %d, want 18", id)
	}
	ends.Apply(Status{ID: 17, Instance: "mole", OldStatus: "active", NewStatus: "listening", Session: "s1"})
	if id, _ := ends.Lookup("mole", "s1"); id != 18 {
		t.Fatalf("an older event moved the turn back: %d", id)
	}
}

func TestTurnEndsResetsOnANewIncarnation(t *testing.T) {
	ends := NewTurnEnds()
	ends.Apply(Status{ID: 20, Instance: "riko", OldStatus: "active", NewStatus: "listening", Session: "old"})
	// Before the new session's first listening entry the fold still holds
	// the old session, which answers only for the old session.
	if id, ok := ends.Lookup("riko", "new"); ok {
		t.Fatalf("new session inherited the old incarnation's turn before its first entry: %d", id)
	}
	if id, ok := ends.Lookup("riko", "old"); !ok || id != 20 {
		t.Fatalf("old session = %d %v, want 20", id, ok)
	}
	ends.Apply(Status{ID: 30, Instance: "riko", OldStatus: "pending", NewStatus: "listening", Session: "new"})
	if id, ok := ends.Lookup("riko", "new"); ok {
		t.Fatalf("new session kept the old incarnation's turn: %d", id)
	}
	if id, ok := ends.Lookup("riko", "old"); ok {
		t.Fatalf("the old session still answers after the reset: %d", id)
	}
	// Very old events carry no session: they never reset the incarnation.
	ends.Apply(Status{ID: 31, Instance: "riko", OldStatus: "active", NewStatus: "listening"})
	if id, _ := ends.Lookup("riko", "new"); id != 31 {
		t.Fatalf("sessionless turn = %d, want 31", id)
	}
	ends.Apply(Status{ID: 40, Instance: "riko", OldStatus: "active", NewStatus: "listening", Session: "new"})
	if id, _ := ends.Lookup("riko", "new"); id != 40 {
		t.Fatalf("turn after reset = %d, want 40", id)
	}
	if _, ok := ends.Lookup("nobody", "new"); ok {
		t.Fatal("unknown agent has a turn")
	}
	var missing *TurnEnds
	if _, ok := missing.Lookup("riko", "new"); ok {
		t.Fatal("nil fold has a turn")
	}
}

func TestTurnEndsNeedsTheExactSession(t *testing.T) {
	ends := NewTurnEnds()
	ends.Apply(Status{ID: 5, Instance: "kiro", OldStatus: "active", NewStatus: "listening", Session: "s1"})
	for _, session := range []string{"", "s2"} {
		if id, ok := ends.Lookup("kiro", session); ok {
			t.Fatalf("session %q got s1's turn %d", session, id)
		}
	}
}
