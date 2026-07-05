// Package claude is the Claude Code harness adapter for bottle: everything
// Claude-specific lives here so the rest of the tool stays harness-agnostic.
//
// It covers four concerns:
//
//   - Discovery — find the session to bottle: the live session via
//     $CLAUDE_CODE_SESSION_ID, or the newest session for a cwd via LastSession.
//   - Encoding — derive Claude's on-disk project directory name from a cwd
//     (EncodeProjectDir). The mapping is lossy and one-way; never invert it.
//   - Materialize — turn a frozen bottle transcript into a fresh, resumable
//     seed session file under ~/.claude/projects/<encoded-cwd>/ (Materialize).
//   - Launch — build the argv that re-enters a materialized seed, either
//     interactively (claude --resume) or in a herdr pane via herder spawn
//     (BuildLaunch). The package only *builds* commands; the cli layer execs
//     them, which keeps launch logic unit-testable with no live spawns.
//
// The projects root is a parameter everywhere it is needed so tests can point
// at a temp directory; the cli layer defaults it to $HOME/.claude/projects.
package claude
