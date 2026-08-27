package claudesession

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"ai-config/tools/herder/internal/hcomidentity"
)

// ResolveReason is a stable refusal category for session path resolution.
type ResolveReason string

const (
	ResolveMissingSession ResolveReason = "missing_session_id"
	ResolveWrongTool      ResolveReason = "tool_not_claude"
	ResolveInvalidSession ResolveReason = "invalid_session_id"
	ResolveFileAbsent     ResolveReason = "session_file_absent"
	ResolveAmbiguousFile  ResolveReason = "session_file_ambiguous"
)

// ResolveError is an honest, typed path-resolution refusal.
type ResolveError struct {
	Reason ResolveReason
	Path   string
	Err    error
}

func (e *ResolveError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("resolve Claude session: %s: %s", e.Reason, e.Path)
	}
	return fmt.Sprintf("resolve Claude session: %s", e.Reason)
}

func (e *ResolveError) Unwrap() error { return e.Err }

func (e *ResolveError) Is(target error) bool {
	t, ok := target.(*ResolveError)
	return ok && e.Reason == t.Reason
}

var sessionIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// Slug encodes a working directory using Claude Code's project-directory
// convention: every non-ASCII-alphanumeric byte becomes '-', with a leading
// '-' added when the encoded value does not already have one.
func Slug(directory string) string {
	var b strings.Builder
	for _, c := range []byte(directory) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if !strings.HasPrefix(out, "-") {
		out = "-" + out
	}
	return out
}

// Resolve locates an agent's session file by immutable session ID. The live
// roster directory is only a fast path: Claude agents routinely change cwd
// during a session, so a missing cwd-derived path falls back to the known
// project directories instead of orphaning an existing transcript.
func Resolve(home string, row hcomidentity.Row) (string, error) {
	if row.Tool != "claude" {
		return "", &ResolveError{Reason: ResolveWrongTool}
	}
	if row.SessionID == "" {
		return "", &ResolveError{Reason: ResolveMissingSession}
	}
	if !sessionIDPattern.MatchString(row.SessionID) {
		return "", &ResolveError{Reason: ResolveInvalidSession}
	}
	path := filepath.Join(home, ".claude", "projects", Slug(row.Directory), row.SessionID+".jsonl")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	pattern := filepath.Join(home, ".claude", "projects", "*", row.SessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", &ResolveError{Reason: ResolveAmbiguousFile, Path: pattern}
	}
	return "", &ResolveError{Reason: ResolveFileAbsent, Path: pattern, Err: os.ErrNotExist}
}
