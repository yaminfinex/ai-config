package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-config/tools/bottle/internal/transcript"
)

// SessionEnvVar is the environment variable Claude Code sets to the live
// session's id inside a running session.
const SessionEnvVar = "CLAUDE_CODE_SESSION_ID"

// ErrNoSessions is returned by LastSession when the cwd's project directory
// holds no candidate session files (absent directory, or only agent-* / non
// .jsonl entries).
var ErrNoSessions = errors.New("no claude sessions found for cwd")

// SelfSessionID returns the live session's id from $CLAUDE_CODE_SESSION_ID, or
// "" when not running inside a Claude session.
func SelfSessionID() string {
	return os.Getenv(SessionEnvVar)
}

// FindSessionPath locates a session's transcript by id. Claude stores a session
// under the project dir encoded from the cwd where it was *launched* (the
// workspace root), so a bottle invoked from a subdirectory cannot assume the
// file sits under the current cwd's encoded dir. preferCwd is tried first — the
// common case, and a tie-breaker if the same id somehow appears twice — then
// every project directory under projectsRoot is searched for <id>.jsonl.
// Session ids are UUIDs (globally unique), so a cross-dir match is unambiguous.
// ErrNoSessions when no project dir holds the file.
func FindSessionPath(projectsRoot, preferCwd, sessionID string) (string, error) {
	if preferCwd != "" {
		p := filepath.Join(ProjectDir(projectsRoot, preferCwd), sessionID+".jsonl")
		if isFile(p) {
			return p, nil
		}
	}
	matches, err := filepath.Glob(filepath.Join(projectsRoot, "*", sessionID+".jsonl"))
	if err != nil {
		return "", err
	}
	for _, m := range matches {
		if isFile(m) {
			return m, nil
		}
	}
	return "", ErrNoSessions
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// SessionPreview describes the session LastSession chose, so the cli can show a
// confirmation before bottling — concurrent same-cwd sessions make a silent
// --last dangerous, so the user/agent gets to see what it picked.
type SessionPreview struct {
	SessionID     string    // the resume id (the seed file's name, sans .jsonl)
	Path          string    // absolute path to the chosen session file
	ModTime       time.Time // file mtime — the "newest" key
	FirstUserText string    // first human turn's text ("" if none/unparseable)
}

// Age reports how old the chosen session is relative to now. The caller passes
// now so callers (and tests) stay in control of the clock.
func (p SessionPreview) Age(now time.Time) time.Duration {
	return now.Sub(p.ModTime)
}

// LastSession finds the newest-mtime session file for cwd under projectsRoot.
// Subagent transcripts (agent-*.jsonl) and non-.jsonl entries are excluded —
// only a main session file can seed a decant. The chosen file's first human
// turn is read for the preview; a parse hiccup there is non-fatal (the preview
// text is simply left empty).
func LastSession(projectsRoot, cwd string) (SessionPreview, error) {
	dir := ProjectDir(projectsRoot, cwd)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return SessionPreview{}, ErrNoSessions
		}
		return SessionPreview{}, err
	}

	var (
		bestName string
		bestMod  time.Time
		found    bool
	)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.HasPrefix(name, "agent-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !found || info.ModTime().After(bestMod) {
			found, bestName, bestMod = true, name, info.ModTime()
		}
	}
	if !found {
		return SessionPreview{}, ErrNoSessions
	}

	path := filepath.Join(dir, bestName)
	preview := SessionPreview{
		SessionID: strings.TrimSuffix(bestName, ".jsonl"),
		Path:      path,
		ModTime:   bestMod,
	}
	if info, err := transcript.IndexFile(path); err == nil {
		if turns := info.Turns(); len(turns) > 0 {
			preview.FirstUserText = turns[0].Text
		}
	}
	return preview, nil
}
