// Package transcript performs all JSONL surgery on Claude Code session files:
// streaming parse and classification of entries, live-branch turn enumeration,
// truncation with trailer repair, and session-id rewriting. It is built
// against the fixture corpus in tools/bottle/testdata and the U1 spike
// findings recorded in that directory's README.
package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Class is the entry taxonomy from the bottle design spec: tree nodes carry
// uuid/parentUuid and form the conversation tree; stateful trailers record
// session state with no uuid of their own; operational lines are bookkeeping
// keyed by other identifiers. Anything unrecognized is ClassUnknown and is
// passed through untouched on copy, dropped after a truncation cut.
type Class int

const (
	ClassUnknown Class = iota
	ClassTreeNode
	ClassTrailer
	ClassOperational
)

func (c Class) String() string {
	switch c {
	case ClassTreeNode:
		return "tree-node"
	case ClassTrailer:
		return "trailer"
	case ClassOperational:
		return "operational"
	default:
		return "unknown"
	}
}

// Entry is the parsed metadata of one transcript line. It deliberately does
// not retain the raw line bytes so an Index over a multi-MB transcript stays
// small; operations that need the bytes re-stream the source.
type Entry struct {
	Line    int    // 1-based line number in the source file
	Type    string // "user", "assistant", "last-prompt", ...
	Subtype string // system subtype: "compact_boundary", "turn_duration", ...

	UUID              string
	ParentUUID        string
	LogicalParentUUID string // set on compact_boundary entries (parentUuid is null there)
	LeafUUID          string // last-prompt and summary entries
	MessageID         string // file-history-snapshot key (the uuid of the user entry it describes)
	SessionID         string // absent on some operational lines (file-history-snapshot)
	Timestamp         string

	IsCompactSummary bool
	IsMeta           bool
	IsSidechain      bool

	// PermissionMode is the session permission mode stamped on this entry:
	// "default", "acceptEdits", "plan", "bypassPermissions", etc. It rides on
	// every user entry and on dedicated permission-mode trailer lines (emitted
	// when the mode is switched mid-session). Empty when the line records none.
	PermissionMode string

	// ToolUseIDs are the tool_use block ids of an assistant entry;
	// ToolResultIDs are the tool_use_ids a user entry carries results for.
	// Exported so U6's self-bottle trim can find unmatched dispatches.
	ToolUseIDs    []string
	ToolResultIDs []string

	userText      string
	hasTextBlock  bool
	hasToolResult bool
	contentIsText bool // message.content was a plain string
}

type wireMessage struct {
	Content json.RawMessage `json:"content"`
}

type wireEntry struct {
	Type              string       `json:"type"`
	Subtype           string       `json:"subtype"`
	UUID              string       `json:"uuid"`
	ParentUUID        string       `json:"parentUuid"`
	LogicalParentUUID string       `json:"logicalParentUuid"`
	LeafUUID          string       `json:"leafUuid"`
	MessageID         string       `json:"messageId"`
	SessionID         string       `json:"sessionId"`
	Timestamp         string       `json:"timestamp"`
	IsCompactSummary  bool         `json:"isCompactSummary"`
	IsMeta            bool         `json:"isMeta"`
	IsSidechain       bool         `json:"isSidechain"`
	PermissionMode    string       `json:"permissionMode"`
	Message           *wireMessage `json:"message"`
}

type contentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	ID        string `json:"id"`
	ToolUseID string `json:"tool_use_id"`
}

// ParseEntry parses a single transcript line. Errors name the 1-based line
// number so callers can surface "line N: ..." directly.
func ParseEntry(line []byte, lineNum int) (Entry, error) {
	var w wireEntry
	if err := json.Unmarshal(line, &w); err != nil {
		return Entry{}, fmt.Errorf("line %d: invalid JSON: %w", lineNum, err)
	}
	trimmed := strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed, "{") {
		return Entry{}, fmt.Errorf("line %d: not a JSON object", lineNum)
	}

	e := Entry{
		Line:              lineNum,
		Type:              w.Type,
		Subtype:           w.Subtype,
		UUID:              w.UUID,
		ParentUUID:        w.ParentUUID,
		LogicalParentUUID: w.LogicalParentUUID,
		LeafUUID:          w.LeafUUID,
		MessageID:         w.MessageID,
		SessionID:         w.SessionID,
		Timestamp:         w.Timestamp,
		IsCompactSummary:  w.IsCompactSummary,
		IsMeta:            w.IsMeta,
		IsSidechain:       w.IsSidechain,
		PermissionMode:    w.PermissionMode,
	}

	if w.Message != nil && len(w.Message.Content) > 0 {
		c := w.Message.Content
		switch c[0] {
		case '"':
			var s string
			if err := json.Unmarshal(c, &s); err != nil {
				return Entry{}, fmt.Errorf("line %d: invalid message content: %w", lineNum, err)
			}
			e.userText = s
			e.contentIsText = true
		case '[':
			var blocks []contentBlock
			if err := json.Unmarshal(c, &blocks); err != nil {
				return Entry{}, fmt.Errorf("line %d: invalid message content: %w", lineNum, err)
			}
			var text strings.Builder
			for _, b := range blocks {
				switch b.Type {
				case "text":
					e.hasTextBlock = true
					text.WriteString(b.Text)
				case "tool_use":
					if b.ID != "" {
						e.ToolUseIDs = append(e.ToolUseIDs, b.ID)
					}
				case "tool_result":
					e.hasToolResult = true
					if b.ToolUseID != "" {
						e.ToolResultIDs = append(e.ToolResultIDs, b.ToolUseID)
					}
				}
			}
			e.userText = text.String()
		}
	}
	return e, nil
}

// Class reports the entry's taxonomy bucket. Unknown types that carry a uuid
// are treated as tree nodes — they participate in the parentUuid topology
// even if this code has never seen their type (forward-compat default).
func (e Entry) Class() Class {
	switch e.Type {
	case "user", "assistant", "system", "attachment":
		return ClassTreeNode
	case "last-prompt", "mode", "permission-mode":
		return ClassTrailer
	case "queue-operation", "file-history-snapshot", "ai-title", "summary":
		return ClassOperational
	}
	if e.UUID != "" {
		return ClassTreeNode
	}
	return ClassUnknown
}

// IsCompactBoundary reports whether the entry is a compaction boundary marker
// (parentUuid null, logicalParentUuid pointing at the pre-compaction leaf).
func (e Entry) IsCompactBoundary() bool {
	return e.Type == "system" && e.Subtype == "compact_boundary"
}

// IsUserTurn reports whether the entry is a human prompt: a user tree node
// carrying human text — not a tool_result carrier, not an isCompactSummary
// or isMeta entry, not a sidechain (subagent) prompt, and not a slash-command
// echo. Command echoes carry no isMeta flag and are only recognizable by
// their <command-...>/<local-command-...> content prefix (U1 census finding).
func (e Entry) IsUserTurn() bool {
	if e.Type != "user" || e.UUID == "" {
		return false
	}
	if e.IsCompactSummary || e.IsMeta || e.IsSidechain {
		return false
	}
	if e.hasToolResult {
		return false
	}
	if !e.contentIsText && !e.hasTextBlock {
		return false
	}
	if e.userText == "" {
		return false
	}
	t := strings.TrimSpace(e.userText)
	if strings.HasPrefix(t, "<command-") || strings.HasPrefix(t, "<local-command-") {
		return false
	}
	return true
}

// UserText returns the human prompt text for user-turn entries, "" otherwise.
func (e Entry) UserText() string {
	if !e.IsUserTurn() {
		return ""
	}
	return e.userText
}
