// Package codexsession resolves and reads Codex rollout JSONL files.
//
// Codex emits several mirrored event rows alongside canonical response items.
// This package follows the rollout census in the fleet-refit mission: event
// user messages are canonical prompts, response assistant messages are
// canonical replies, and duplicate agent/user event or response rows are not
// served twice.
package codexsession
