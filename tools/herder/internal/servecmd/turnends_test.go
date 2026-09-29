package servecmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
)

func TestFoldBoardTurnEndsStampsPlacedUnplacedAndSubagentsByBaseName(t *testing.T) {
	ends := hcomevents.NewTurnEnds()
	ends.Apply(hcomevents.Status{ID: 41, Instance: "dore", OldStatus: "active", NewStatus: "listening", Session: "s1"})
	ends.Apply(hcomevents.Status{ID: 42, Instance: "vava", OldStatus: "active", NewStatus: "listening", Session: "s2"})
	ends.Apply(hcomevents.Status{ID: 43, Instance: "kiro", OldStatus: "blocked", NewStatus: "listening", Session: "s3"})
	roster := []hcomidentity.Row{
		{Name: "impl-dore", BaseName: "dore", SessionID: "s1"},
		{Name: "vava", BaseName: "vava", SessionID: "s2"},
		{Name: "review-kiro", BaseName: "kiro", SessionID: "s3"},
	}
	subagents := fleetview.Rows{{Agent: "review-kiro"}}
	board := fleetview.Board{
		Workspaces: []fleetview.Workspace{{Tabs: []fleetview.Tab{{Panes: []fleetview.Pane{
			{Agent: "impl-dore", Subagents: []fleetview.Row{{Agent: "review-kiro"}}},
			{Agent: ""},
		}}}}},
		Unplaced: []fleetview.Row{{Agent: "vava", Subagents: &subagents}, {Agent: "quiet"}},
	}
	foldBoardTurnEnds(&board, roster, ends)
	panes := board.Workspaces[0].Tabs[0].Panes
	if panes[0].TurnEndID != 41 || panes[0].Subagents[0].TurnEndID != 43 || panes[1].TurnEndID != 0 {
		t.Fatalf("placed = %#v", panes)
	}
	if board.Unplaced[0].TurnEndID != 42 || (*board.Unplaced[0].Subagents)[0].TurnEndID != 43 || board.Unplaced[1].TurnEndID != 0 {
		t.Fatalf("unplaced = %#v", board.Unplaced)
	}
	raw, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"turn_end_id":41`) || strings.Count(string(raw), "turn_end_id") != 4 {
		t.Fatalf("json = %s", raw)
	}
	foldBoardTurnEnds(&board, roster, nil)
}

func TestStartTurnEndsFoldsTheSubscriptionIntoTheBoard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := fixtureDeps()
	folded := make(chan struct{})
	calls := 0
	deps.listening = func(ctx context.Context, cursor *hcomevents.Cursor, emit func(hcomevents.Status) error) error {
		calls++
		if calls == 1 {
			// A failed subscription retries with the same cursor.
			return context.DeadlineExceeded
		}
		for _, status := range []hcomevents.Status{
			{ID: 50, Instance: "dore", OldStatus: "active", NewStatus: "listening", Session: "session-dore"},
			{ID: 51, Instance: "dore", OldStatus: "listening", NewStatus: "listening", Session: "session-dore"},
		} {
			if err := emit(status); err != nil {
				return err
			}
		}
		close(folded)
		<-ctx.Done()
		return nil
	}
	deps.audit = func(string, ...any) {}
	deps.turnEnds = startTurnEnds(ctx, deps)
	select {
	case <-folded:
	case <-time.After(time.Second):
		t.Fatal("turn ends did not fold")
	}
	board, err := readBoard(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if got := board.Workspaces[0].Tabs[0].Panes[0].TurnEndID; got != 50 {
		t.Fatalf("placed turn_end_id = %d, want 50", got)
	}
}

// A new incarnation, before its first turn end or while it is active when
// the serve restarts, must not inherit the previous session's turn.
func TestFoldBoardTurnEndsBelongToTheCurrentRosterIncarnation(t *testing.T) {
	ends := hcomevents.NewTurnEnds()
	ends.Apply(hcomevents.Status{ID: 41, Instance: "mole", OldStatus: "active", NewStatus: "listening", Session: "old-session"})
	roster := []hcomidentity.Row{{Name: "impl-mole", BaseName: "mole", SessionID: "new-session", Status: "active"}}
	board := fleetview.Board{Unplaced: []fleetview.Row{{Agent: "impl-mole", BusStatus: "active"}}}
	foldBoardTurnEnds(&board, roster, ends)
	if got := board.Unplaced[0].TurnEndID; got != 0 {
		t.Fatalf("new-session received old-session turn_end_id=%d", got)
	}
	ends.Apply(hcomevents.Status{ID: 60, Instance: "mole", OldStatus: "active", NewStatus: "listening", Session: "new-session"})
	foldBoardTurnEnds(&board, roster, ends)
	if got := board.Unplaced[0].TurnEndID; got != 60 {
		t.Fatalf("new-session's own turn = %d, want 60", got)
	}
}

func TestFoldBoardTurnEndsDistinctSessionsSharingABaseNameDoNotShare(t *testing.T) {
	ends := hcomevents.NewTurnEnds()
	ends.Apply(hcomevents.Status{ID: 41, Instance: "mole", OldStatus: "active", NewStatus: "listening", Session: "session-one"})
	roster := []hcomidentity.Row{
		{Name: "impl-mole", BaseName: "mole", SessionID: "session-one"},
		{Name: "review-mole", BaseName: "mole", SessionID: "session-two"},
	}
	board := fleetview.Board{Unplaced: []fleetview.Row{{Agent: "impl-mole"}, {Agent: "review-mole"}, {Agent: "not-in-roster"}}}
	foldBoardTurnEnds(&board, roster, ends)
	if got := board.Unplaced[0].TurnEndID; got != 41 {
		t.Fatalf("session-one = %d, want 41", got)
	}
	if got := board.Unplaced[1].TurnEndID; got != 0 {
		t.Fatalf("session-two received session-one turn_end_id=%d", got)
	}
	if got := board.Unplaced[2].TurnEndID; got != 0 {
		t.Fatalf("a row outside the roster got a turn: %d", got)
	}
}
