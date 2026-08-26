package codexsession

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"ai-config/tools/herder/internal/hcomidentity"
)

// ResolveReason is a stable refusal category for rollout path resolution.
type ResolveReason string

const (
	ResolveMissingSession ResolveReason = "missing_session_id"
	ResolveWrongTool      ResolveReason = "tool_not_codex"
	ResolveInvalidSession ResolveReason = "invalid_session_id"
	ResolveFileAbsent     ResolveReason = "session_file_absent"
	ResolveAmbiguousFile  ResolveReason = "session_file_ambiguous"
)

// ResolveError is an honest, typed path-resolution refusal.
type ResolveError struct {
	Reason  ResolveReason
	Pattern string
	Err     error
}

func (e *ResolveError) Error() string {
	if e.Pattern != "" {
		return fmt.Sprintf("resolve Codex session: %s: %s", e.Reason, e.Pattern)
	}
	return fmt.Sprintf("resolve Codex session: %s", e.Reason)
}

func (e *ResolveError) Unwrap() error { return e.Err }

func (e *ResolveError) Is(target error) bool {
	t, ok := target.(*ResolveError)
	return ok && e.Reason == t.Reason
}

var sessionIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// Resolve finds the one dated rollout whose filename ends in the roster
// session ID. The fixed three-directory glob is the Codex on-disk contract;
// it never searches outside ~/.codex/sessions/YYYY/MM/DD.
func Resolve(home string, row hcomidentity.Row) (string, error) {
	if row.Tool != "codex" {
		return "", &ResolveError{Reason: ResolveWrongTool}
	}
	if row.SessionID == "" {
		return "", &ResolveError{Reason: ResolveMissingSession}
	}
	if !sessionIDPattern.MatchString(row.SessionID) {
		return "", &ResolveError{Reason: ResolveInvalidSession}
	}
	pattern := filepath.Join(home, ".codex", "sessions", "*", "*", "*", "rollout-*-"+row.SessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", &ResolveError{Reason: ResolveFileAbsent, Pattern: pattern, Err: os.ErrNotExist}
	}
	if len(matches) != 1 {
		return "", &ResolveError{Reason: ResolveAmbiguousFile, Pattern: pattern}
	}
	return matches[0], nil
}
