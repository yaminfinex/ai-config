package servecmd

import (
	"encoding/json"
	"testing"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
)

var replyEpoch = time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)

func entry(kind claudesession.Kind, second int, text string) claudesession.Entry {
	payload := json.RawMessage(`{}`)
	if text != "" {
		payload, _ = json.Marshal(map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}})
	}
	at := replyEpoch.Add(time.Duration(second)*time.Second + 300*time.Millisecond)
	return claudesession.Entry{Kind: kind, Timestamp: at.Format(time.RFC3339Nano), Payload: payload}
}

// turn is one turn ending at second end: an hcom wake, a tool call, the
// final text, then the stop hook chip and the end marker.
func turn(end int, text string) []claudesession.Entry {
	return []claudesession.Entry{
		entry(claudesession.KindHcomStub, end-5, ""),
		entry(claudesession.KindToolUse, end-4, ""),
		entry(claudesession.KindToolResult, end-3, ""),
		entry(claudesession.KindAssistantText, end, text),
		entry(claudesession.KindSystemChip, end, ""),
		entry(claudesession.KindTurnDuration, end, ""),
	}
}

type replyFixture struct {
	t       *testing.T
	ends    *hcomevents.TurnEnds
	replies *replyEnds
	tail    []claudesession.Entry
	reads   int
	roster  []hcomidentity.Row
}

func newReplyFixture(t *testing.T) *replyFixture {
	f := &replyFixture{t: t, ends: hcomevents.NewTurnEnds(), roster: []hcomidentity.Row{{Name: "impl-dore", BaseName: "dore", Tool: "claude", SessionID: "s1"}}}
	f.replies = newReplyEnds(func(hcomidentity.Row) ([]claudesession.Entry, error) { f.reads++; return f.tail, nil })
	return f
}

// end is the bus seeing turn id end at second (hcom's ts drops the fraction).
func (f *replyFixture) end(id int64, second int) {
	ts := replyEpoch.Add(time.Duration(second) * time.Second).Format("2006-01-02T15:04:05")
	f.ends.Apply(hcomevents.Status{ID: id, TS: ts, Instance: "dore", OldStatus: "active", NewStatus: "listening", Session: "s1"})
}

func (f *replyFixture) board() fleetview.Row {
	board := fleetview.Board{Unplaced: []fleetview.Row{{Agent: "impl-dore"}}}
	foldBoardTurnEnds(&board, f.roster, f.ends, f.replies)
	return board.Unplaced[0]
}

func TestReplyEndsAdvanceOnlyOnPlainTextTurns(t *testing.T) {
	f := newReplyFixture(t)
	f.tail = turn(100, "Done: merged the fix.")
	f.end(10, 100)
	if row := f.board(); row.TurnEndID != 10 || row.ReplyEndID != 10 {
		t.Fatalf("plain-text turn = %#v", row)
	}
	f.tail = append(f.tail, turn(200, "<status>still waiting on the reviewer</status>")...)
	f.end(11, 200)
	if row := f.board(); row.TurnEndID != 11 || row.ReplyEndID != 10 {
		t.Fatalf("status-only turn = %#v", row)
	}
	f.tail = append(f.tail, turn(300, "<internal>checked the logs</internal>\n<status>idle</status>\n")...)
	f.end(12, 300)
	if row := f.board(); row.ReplyEndID != 10 {
		t.Fatalf("internal and status turn = %#v", row)
	}
	// A turn end is judged once: a later board does not read again.
	before := f.reads
	if row := f.board(); row.ReplyEndID != 10 || f.reads != before {
		t.Fatalf("rejudged: reply %d, reads %d -> %d", row.ReplyEndID, before, f.reads)
	}
	// A turn that ended on a tool, with no final text, is not a reply.
	f.tail = append(f.tail, entry(claudesession.KindAssistantText, 395, "Looking."), entry(claudesession.KindToolUse, 396, ""), entry(claudesession.KindToolResult, 399, ""), entry(claudesession.KindTurnDuration, 400, ""))
	f.end(13, 400)
	if row := f.board(); row.ReplyEndID != 10 {
		t.Fatalf("tool-ended turn = %#v", row)
	}
}

// Review finding 1: a turn whose own end marker has not landed waits for it,
// whatever the roster says, and never borrows the previous turn's marker.
func TestReplyEndsWaitForTheTurnsOwnMarker(t *testing.T) {
	f := newReplyFixture(t)
	f.tail = turn(100, "Done: merged the fix.")
	f.end(10, 100)
	if row := f.board(); row.ReplyEndID != 10 {
		t.Fatalf("plain-text turn = %#v", row)
	}
	f.roster[0].Status = "active"
	landing := turn(200, "<status>Waiting.</status>")
	f.tail = append(f.tail, landing[:4]...)
	f.end(11, 200)
	if row := f.board(); row.TurnEndID != 11 || row.ReplyEndID != 10 {
		t.Fatalf("unlanded status-only turn = %#v", row)
	}
	f.tail = append(f.tail, landing[4:]...)
	f.roster[0].Status = "listening"
	if row := f.board(); row.ReplyEndID != 10 {
		t.Fatalf("landed status-only turn = %#v", row)
	}
	before := f.reads
	f.board()
	if f.reads != before {
		t.Fatal("a landed turn was read again")
	}
}

// Review finding 2: a plain reply followed by a status-only turn before the
// next board is still the reply.
func TestReplyEndsJudgeEveryTurnSinceTheLastBoard(t *testing.T) {
	f := newReplyFixture(t)
	f.tail = turn(100, "<status>started</status>")
	f.end(10, 100)
	if row := f.board(); row.ReplyEndID != 0 {
		t.Fatalf("status-only first turn = %#v", row)
	}
	f.tail = append(f.tail, turn(200, "Please approve the migration.")...)
	f.end(11, 200)
	f.tail = append(f.tail, turn(300, "<status>waiting on approval</status>")...)
	f.end(12, 300)
	if row := f.board(); row.TurnEndID != 12 || row.ReplyEndID != 11 {
		t.Fatalf("plain turn between boards = %#v", row)
	}
}

func TestReplyEndsJudgeTurnsOlderThanTheTailAsNoReply(t *testing.T) {
	f := newReplyFixture(t)
	f.end(10, 50) // A serve's catch-up: older than anything in the tail.
	f.tail = turn(200, "The PR is up.")
	f.end(11, 200)
	if row := f.board(); row.ReplyEndID != 11 {
		t.Fatalf("out-of-reach then plain = %#v", row)
	}
}

func TestReplyEndsCountEveryTurnWithoutATranscript(t *testing.T) {
	f := newReplyFixture(t)
	f.roster[0].Tool = "gemini"
	f.replies.tail = func(hcomidentity.Row) ([]claudesession.Entry, error) {
		t.Fatal("read a tool with no reader")
		return nil, nil
	}
	f.end(7, 100)
	if row := f.board(); row.ReplyEndID != 7 {
		t.Fatalf("gemini reply = %#v", row)
	}
}

func TestHasVisibleTextMatchesTheWebFencing(t *testing.T) {
	for text, want := range map[string]bool{
		"":                          false,
		"  \n":                      false,
		"plain":                     true,
		"<status>busy</status>":     false,
		"<status>busy</status>\n\n": false,
		"<internal>a\nb</internal><status>x</status>": false,
		"<status>busy</status> and a reply":           true,
		"before <internal>x</internal>":               true,
		"<status>two\nlines</status>":                 true, // malformed: literal
		"</status>":                                   true,
		"<status>unclosed":                            true,
		"<status>x</internal>":                        true,
	} {
		if got := hasVisibleText(text); got != want {
			t.Errorf("hasVisibleText(%q) = %v, want %v", text, got, want)
		}
	}
	if got := assistantText(json.RawMessage(`{"isApiErrorMessage":true,"message":{"content":[{"type":"text","text":"x"}]}}`)); got != "" {
		t.Fatalf("api error text = %q", got)
	}
	if got := assistantText(json.RawMessage(`{"content":"bare"}`)); got != "bare" {
		t.Fatalf("string content = %q", got)
	}
}
