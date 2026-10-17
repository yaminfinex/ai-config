package servecmd

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
)

// replyTailEntries is how many renderable entries a reply read takes from
// the transcript's end: enough to reach back from the turn's end marker to
// its final assistant text.
const replyTailEntries = 40

// replyEnds stamps reply_end_id: the turn-end id of the agent's latest turn
// that ended with plain text, so a turn ending with only <status> or
// <internal> leaves it where it was. Each turn end is judged once, from the
// transcript tail, the first time a board sees it; a turn whose end has not
// landed in the transcript yet is judged again on the next board.
type replyEnds struct {
	tail   func(hcomidentity.Row) ([]claudesession.Entry, error)
	mu     sync.Mutex
	agents map[string]replyEnd // instance + "\x00" + session
}

type replyEnd struct {
	turn  int64 // The turn end last judged.
	reply int64 // The latest turn end judged plain text.
}

func newReplyEnds(tail func(hcomidentity.Row) ([]claudesession.Entry, error)) *replyEnds {
	return &replyEnds{tail: tail, agents: map[string]replyEnd{}}
}

func readReplyTail(row hcomidentity.Row) ([]claudesession.Entry, error) {
	path, err := entrySessionPath(row)
	if err != nil {
		return nil, err
	}
	read, _, err := readEntryTail(path, row, replyTailEntries)
	return read.Entries, err
}

// lookup returns the reply end for the row's incarnation given its current
// turn end. A tool with no transcript reader, or a failed read, counts the
// turn as a reply, as every turn counted before.
func (r *replyEnds) lookup(row hcomidentity.Row, instance string, turn int64) int64 {
	if r == nil || turn <= 0 {
		return 0
	}
	key := instance + "\x00" + row.SessionID
	r.mu.Lock()
	current := r.agents[key]
	r.mu.Unlock()
	if current.turn == turn {
		return current.reply
	}
	plain, landed := true, true
	if row.Tool == "claude" || row.Tool == "codex" {
		entries, err := r.tail(row)
		if err == nil {
			plain, landed = turnReply(entries, row.Status == "listening")
		}
	}
	if !landed {
		return current.reply
	}
	current.turn = turn
	if plain {
		current.reply = turn
	}
	r.mu.Lock()
	r.agents[key] = current
	r.mu.Unlock()
	return current.reply
}

// turnReply judges the turn that ended last in a transcript tail: plain is
// whether its final assistant text keeps visible text once fences are
// dropped; landed is false while its end marker (Claude turn_duration,
// Codex task_complete) is not written yet. While the agent is listening,
// activity after the newest end marker is the ended turn still landing;
// otherwise it is the next turn begun, and the marker before it ends ours
// (out of the tail's reach, the turn counts as no reply rather than being
// read again on every board while the next one runs).
func turnReply(entries []claudesession.Entry, listening bool) (plain, landed bool) {
	index := len(entries) - 1
	for ; index >= 0; index-- {
		kind := entries[index].Kind
		if kind == claudesession.KindTurnDuration {
			break
		}
		if listening && turnActivity(kind) {
			return false, false
		}
	}
	if index < 0 {
		return false, !listening
	}
	for index--; index >= 0; index-- {
		kind := entries[index].Kind
		if kind == claudesession.KindAssistantText {
			return hasVisibleText(assistantText(entries[index].Payload)), true
		}
		if kind == claudesession.KindTurnDuration || turnActivity(kind) {
			return false, true
		}
	}
	return false, true
}

func turnActivity(kind claudesession.Kind) bool {
	switch kind {
	case claudesession.KindAssistantText, claudesession.KindThinking, claudesession.KindToolUse, claudesession.KindToolResult,
		claudesession.KindHumanPrompt, claudesession.KindHcomStub, claudesession.KindHcomDelivery,
		claudesession.KindTaskNotification, claudesession.KindChannelMessage:
		return true
	}
	return false
}

// assistantText is an assistant entry's text blocks joined, as the web's
// answerText reads them; an API error is not an answer.
func assistantText(payload json.RawMessage) string {
	var entry struct {
		IsAPIError bool            `json:"isApiErrorMessage"`
		Content    json.RawMessage `json:"content"`
		Message    struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(payload, &entry) != nil || entry.IsAPIError {
		return ""
	}
	content := entry.Message.Content
	if len(content) == 0 {
		content = entry.Content
	}
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(content, &blocks)
	parts := []string{}
	for _, block := range blocks {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var fenceTag = regexp.MustCompile(`</?(?:internal|status)>`)

// hasVisibleText is the web's parseAssistantFencing hasVisibleText: text
// outside well-formed <internal> and one-line <status> fences; malformed
// fencing is read as literal text.
func hasVisibleText(content string) bool {
	literal := strings.TrimSpace(content) != ""
	var visible strings.Builder
	cursor, open, bodyStart := 0, "", 0
	for _, match := range fenceTag.FindAllStringIndex(content, -1) {
		tag := content[match[0]:match[1]]
		closing := strings.HasPrefix(tag, "</")
		kind := "status"
		if strings.Contains(tag, "internal") {
			kind = "internal"
		}
		if open == "" {
			if closing {
				return literal
			}
			visible.WriteString(content[cursor:match[0]])
			open, bodyStart = kind, match[1]
			continue
		}
		if !closing || kind != open || (kind == "status" && strings.ContainsAny(content[bodyStart:match[0]], "\r\n")) {
			return literal
		}
		cursor, open = match[1], ""
	}
	if open != "" {
		return literal
	}
	visible.WriteString(content[cursor:])
	return strings.TrimSpace(visible.String()) != ""
}
