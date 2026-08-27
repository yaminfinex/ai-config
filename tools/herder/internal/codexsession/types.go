package codexsession

import "ai-config/tools/herder/internal/claudesession"

// Codex and Claude deliberately share one rendering taxonomy and cursor wire
// model. Aliases make drift a compile-time failure while each package retains
// its own resolver and rollout parser.
type (
	Kind         = claudesession.Kind
	Quarantine   = claudesession.Quarantine
	Entry        = claudesession.Entry
	Stats        = claudesession.Stats
	ReadResult   = claudesession.ReadResult
	Cursor       = claudesession.Cursor
	ResetReason  = claudesession.ResetReason
	Reset        = claudesession.Reset
	TailResult   = claudesession.TailResult
	PairedEntry  = claudesession.PairedEntry
	ContextUsage = claudesession.ContextUsage
	Vitals       = claudesession.Vitals
)

const (
	KindHumanPrompt      = claudesession.KindHumanPrompt
	KindHcomStub         = claudesession.KindHcomStub
	KindHcomDelivery     = claudesession.KindHcomDelivery
	KindTaskNotification = claudesession.KindTaskNotification
	KindInjectedSystem   = claudesession.KindInjectedSystem
	KindCommandOutput    = claudesession.KindCommandOutput
	KindCompactDivider   = claudesession.KindCompactDivider
	KindAssistantText    = claudesession.KindAssistantText
	KindThinking         = claudesession.KindThinking
	KindToolUse          = claudesession.KindToolUse
	KindToolResult       = claudesession.KindToolResult
	KindTurnDuration     = claudesession.KindTurnDuration
	KindSystemChip       = claudesession.KindSystemChip
	KindUnknown          = claudesession.KindUnknown

	ResetTruncated      = claudesession.ResetTruncated
	ResetSessionChanged = claudesession.ResetSessionChanged
)
