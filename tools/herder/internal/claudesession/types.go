package claudesession

import (
	"encoding/json"
	"fmt"
)

// Kind is the stable rendering classification for a session entry.
type Kind string

const (
	KindHumanPrompt      Kind = "human_prompt"
	KindHcomStub         Kind = "hcom_delivery_stub"
	KindHcomDelivery     Kind = "hcom_delivery"
	KindTaskNotification Kind = "task_notification"
	KindInjectedSystem   Kind = "injected_system"
	KindCommandOutput    Kind = "command_stdout"
	KindCompactDivider   Kind = "compact_divider"
	KindAssistantText    Kind = "assistant_text"
	KindThinking         Kind = "thinking"
	KindToolUse          Kind = "tool_use"
	KindToolResult       Kind = "tool_result"
	KindTurnDuration     Kind = "turn_duration"
	KindSystemChip       Kind = "system_chip"
	KindUnknown          Kind = "unknown"
)

// Quarantine records why a complete line could not be decoded. Such lines
// remain visible as unknown entries instead of failing the whole read.
type Quarantine struct {
	Reason string `json:"reason"`
}

// Entry is an immutable, one-line rendering record. Payload is always valid
// JSON; unknown entries retain the complete raw object.
type Entry struct {
	UUID       string          `json:"uuid,omitempty"`
	Line       int64           `json:"line"`
	ByteOffset int64           `json:"byte_offset"`
	Timestamp  string          `json:"timestamp,omitempty"`
	Kind       Kind            `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
	Quarantine *Quarantine     `json:"quarantine,omitempty"`
}

// Stats reports deliberately skipped input without pretending it was absent.
type Stats struct {
	SidechainSkipped int `json:"sidechain_skipped"`
}

// ContextUsage is the latest context-bearing usage record in a session. The
// optional source-specific fields stay absent rather than implying zero when a
// tool does not report them.
type ContextUsage struct {
	UsedTokens               int64    `json:"used_tokens"`
	InputTokens              int64    `json:"input_tokens"`
	CachedInputTokens        *int64   `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens *int64   `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int64   `json:"cache_read_input_tokens,omitempty"`
	OutputTokens             *int64   `json:"output_tokens,omitempty"`
	WindowTokens             *int64   `json:"window_tokens,omitempty"`
	UsedPercent              *float64 `json:"used_percent,omitempty"`
}

// Vitals contains the latest independently observed model and context facts.
type Vitals struct {
	Model        string        `json:"model,omitempty"`
	ContextUsage *ContextUsage `json:"context_usage,omitempty"`
}

// ReadResult is one complete-lines-only read from a byte offset.
type ReadResult struct {
	Entries    []Entry `json:"entries"`
	NextOffset int64   `json:"next_offset"`
	Stats      Stats   `json:"stats"`
}

// Cursor binds a tail position to the session identity that produced it.
type Cursor struct {
	SessionID string `json:"session_id"`
	Offset    int64  `json:"offset"`
}

// ResetReason says why a consumer must discard its entry view and cursor.
type ResetReason string

const (
	ResetTruncated      ResetReason = "truncated"
	ResetSessionChanged ResetReason = "session_changed"
)

// Reset is an explicit tail discontinuity, never an implicit replay.
type Reset struct {
	Reason            ResetReason `json:"reason"`
	PreviousSessionID string      `json:"previous_session_id,omitempty"`
	SessionID         string      `json:"session_id"`
	PreviousOffset    int64       `json:"previous_offset"`
}

func (r Reset) Error() string {
	return fmt.Sprintf("claude session tail reset: %s", r.Reason)
}

// TailResult contains either new entries or a Reset, never both.
type TailResult struct {
	Read   ReadResult `json:"read"`
	Cursor Cursor     `json:"cursor"`
	Reset  *Reset     `json:"reset,omitempty"`
}

// PairedEntry is a consumer-side view over immutable entries.
type PairedEntry struct {
	Primary Entry  `json:"primary"`
	Related *Entry `json:"related,omitempty"`
}
