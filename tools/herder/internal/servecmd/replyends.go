package servecmd

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
)

// replyTailEntries is how many renderable entries a reply read takes from
// the transcript's end: enough to reach back from the turn's end marker to
// its final assistant text.
const replyTailEntries = 40

// replyEnds stamps reply_end_id: the turn-end id of the agent's latest turn
// that ended with plain text, so a turn ending with only <status> or
// <internal> leaves it where it was. Every turn end the status subscriber
// kept is judged once, oldest first, against its own end marker in the
// transcript tail; one whose marker has not landed yet waits, with every
// later one, for the next board.
type replyEnds struct {
	tail   func(hcomidentity.Row) ([]claudesession.Entry, error)
	mu     sync.Mutex
	agents map[string]replyEnd // instance + "\x00" + session
}

type replyEnd struct {
	judged int64 // The newest turn end judged.
	reply  int64 // The newest turn end judged plain text.
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

// lookup judges the row's incarnation's turn ends not judged yet and
// returns its reply end. A tool with no transcript reader, or a failed
// read, counts every turn as a reply, as every turn counted before.
func (r *replyEnds) lookup(row hcomidentity.Row, instance string, ends *hcomevents.TurnEnds) int64 {
	if r == nil {
		return 0
	}
	key := instance + "\x00" + row.SessionID
	r.mu.Lock()
	current := r.agents[key]
	r.mu.Unlock()
	turns := ends.Since(instance, row.SessionID, current.judged)
	if len(turns) == 0 {
		return current.reply
	}
	var entries []claudesession.Entry
	err := errNoReader
	if row.Tool == "claude" || row.Tool == "codex" {
		entries, err = r.tail(row)
	}
	if err != nil {
		last := turns[len(turns)-1].ID
		current = replyEnd{judged: last, reply: last}
	} else {
		current = judgeTurns(entries, turns, current)
	}
	r.mu.Lock()
	r.agents[key] = current
	r.mu.Unlock()
	return current.reply
}

var errNoReader = errors.New("no transcript reader")

// judgeTurns matches turn ends, oldest first, to end markers (Claude
// turn_duration, Codex task_complete): each turn's marker is the first one
// at or after its event time and after the previous turn's marker, and the
// turn is a reply when its final assistant text keeps visible text. A turn
// whose marker is not in the tail stops the walk (not landed yet); one older
// than the whole tail is out of reach and judged no reply.
// hcom times are to the second, so two turns ending within one second can match one marker off.
func judgeTurns(entries []claudesession.Entry, turns []hcomevents.Turn, current replyEnd) replyEnd {
	next := 0
	for _, turn := range turns {
		if len(entries) > 0 && !entryTime(entries[0]).Before(turn.At) {
			current.judged = turn.ID
			continue
		}
		marker := -1
		for index := next; index < len(entries); index++ {
			if entries[index].Kind == claudesession.KindTurnDuration && !entryTime(entries[index]).Before(turn.At) {
				marker = index
				break
			}
		}
		if marker < 0 {
			break
		}
		if finalText(entries[:marker]) {
			current.reply = turn.ID
		}
		current.judged, next = turn.ID, marker+1
	}
	return current
}

// finalText is whether the turn ending at the end of entries ended with
// plain text: its last assistant text, with nothing but chips after it.
func finalText(entries []claudesession.Entry) bool {
	for index := len(entries) - 1; index >= 0; index-- {
		kind := entries[index].Kind
		if kind == claudesession.KindAssistantText {
			return hasVisibleText(assistantText(entries[index].Payload))
		}
		if kind == claudesession.KindTurnDuration || turnActivity(kind) {
			return false
		}
	}
	return false
}

func entryTime(entry claudesession.Entry) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)
	return at
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
