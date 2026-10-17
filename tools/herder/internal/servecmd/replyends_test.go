package servecmd

import (
	"encoding/json"
	"testing"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
)

func entry(kind claudesession.Kind, text string) claudesession.Entry {
	payload := json.RawMessage(`{}`)
	if text != "" {
		payload, _ = json.Marshal(map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}})
	}
	return claudesession.Entry{Kind: kind, Payload: payload}
}

func turn(text string) []claudesession.Entry {
	return []claudesession.Entry{
		entry(claudesession.KindHcomStub, ""),
		entry(claudesession.KindToolUse, ""),
		entry(claudesession.KindToolResult, ""),
		entry(claudesession.KindAssistantText, text),
		entry(claudesession.KindSystemChip, ""),
		entry(claudesession.KindTurnDuration, ""),
	}
}

func TestReplyEndsAdvanceOnlyOnPlainTextTurns(t *testing.T) {
	ends := hcomevents.NewTurnEnds()
	tail := []claudesession.Entry{}
	reads := 0
	replies := newReplyEnds(func(hcomidentity.Row) ([]claudesession.Entry, error) { reads++; return tail, nil })
	roster := []hcomidentity.Row{{Name: "impl-dore", BaseName: "dore", Tool: "claude", Status: "listening", SessionID: "s1"}}
	fold := func(id int64) fleetview.Row {
		ends.Apply(hcomevents.Status{ID: id, Instance: "dore", OldStatus: "active", NewStatus: "listening", Session: "s1"})
		board := fleetview.Board{Unplaced: []fleetview.Row{{Agent: "impl-dore"}}}
		foldBoardTurnEnds(&board, roster, ends, replies)
		return board.Unplaced[0]
	}

	tail = turn("Done: merged the fix.")
	if row := fold(10); row.TurnEndID != 10 || row.ReplyEndID != 10 {
		t.Fatalf("plain-text turn = %#v", row)
	}
	tail = append(tail, turn("<status>still waiting on the reviewer</status>")...)
	if row := fold(11); row.TurnEndID != 11 || row.ReplyEndID != 10 {
		t.Fatalf("status-only turn = %#v", row)
	}
	tail = append(tail, turn("<internal>checked the logs</internal>\n<status>idle</status>\n")...)
	if row := fold(12); row.ReplyEndID != 10 {
		t.Fatalf("internal and status turn = %#v", row)
	}
	// A turn's end is judged once: a later board does not read again.
	before := reads
	if row := fold(12); row.ReplyEndID != 10 || reads != before {
		t.Fatalf("rejudged: reply %d, reads %d -> %d", row.ReplyEndID, before, reads)
	}

	// The turn ended on the bus before its text and end marker landed: the
	// reply stays where it was and the turn is read again on the next board.
	landed := turn("<status>shipped</status> The PR is up.")
	tail = append(tail, landed[:3]...)
	if row := fold(13); row.TurnEndID != 13 || row.ReplyEndID != 10 {
		t.Fatalf("unlanded turn = %#v", row)
	}
	tail = append(tail, landed[3:]...)
	if row := fold(13); row.ReplyEndID != 13 {
		t.Fatalf("landed turn = %#v", row)
	}
}

func TestReplyEndsJudgeTheEndedTurnOnceTheNextHasBegun(t *testing.T) {
	tail := append(turn("Here is the plan."), entry(claudesession.KindHcomStub, ""), entry(claudesession.KindThinking, ""))
	if plain, landed := turnReply(tail, false); !plain || !landed {
		t.Fatalf("active agent: plain %v landed %v", plain, landed)
	}
	if _, landed := turnReply(tail, true); landed {
		t.Fatal("listening agent with activity past the end marker counted as landed")
	}
	if _, landed := turnReply(turn("x")[:4], true); landed {
		t.Fatal("no end marker counted as landed while listening")
	}
	if plain, landed := turnReply(turn("x")[:4], false); plain || !landed {
		t.Fatalf("end marker out of reach while active: plain %v landed %v", plain, landed)
	}
	// A turn that ended on a tool, with no final text, is not a reply.
	noText := []claudesession.Entry{entry(claudesession.KindAssistantText, "Looking."), entry(claudesession.KindToolUse, ""), entry(claudesession.KindToolResult, ""), entry(claudesession.KindTurnDuration, "")}
	if plain, landed := turnReply(noText, true); plain || !landed {
		t.Fatalf("tool-ended turn: plain %v landed %v", plain, landed)
	}
}

func TestReplyEndsCountEveryTurnWithoutATranscript(t *testing.T) {
	replies := newReplyEnds(func(hcomidentity.Row) ([]claudesession.Entry, error) {
		t.Fatal("read a tool with no reader")
		return nil, nil
	})
	if got := replies.lookup(hcomidentity.Row{Tool: "gemini", SessionID: "s"}, "gena", 7); got != 7 {
		t.Fatalf("gemini reply = %d", got)
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
