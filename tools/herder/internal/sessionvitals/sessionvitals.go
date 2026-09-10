// Package sessionvitals is the ONE reader that turns a session transcript
// into live model and context vitals, and the ONE lookup body that decides
// where the CLI and the serve get them from.
//
// Shape: one reader (ReadDirect over claudesession/codexsession's exported
// ObserveVitals), one lookup body (ReadWith: try a cache, else read direct),
// two caches (the observer in-process inside the serve, the herdersock socket
// out-of-process for the CLI), and the direct read behind both. Nothing here
// is persisted; the transcript remains the authority behind every answer.
//
// Seed and Advance (tail.go) are the incremental primitives the observer
// builds on; they use the same parse functions, so the cache can never drift
// from a direct read.
package sessionvitals

import (
	"errors"
	"fmt"
	"os"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/codexsession"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdersock"
	"ai-config/tools/herder/internal/herderstate"
)

// Result is one answer to "what are this session's vitals right now".
// Source is herdersock.SourceCache when a cache answered (ObservedAt is then
// the observer's stamp) or herdersock.SourceDirect when the transcript was
// read here (ObservedAt is the file's mtime, as before).
type Result struct {
	Vitals     claudesession.Vitals
	Path       string
	ObservedAt time.Time
	Source     string
}

// Lookup is a cache in front of the direct read: it answers (result, true)
// only when it holds vitals for the row's session. The observer's Lookup and
// the socket client are the two implementations; the miss rule (for example
// "no vitals yet" is a miss, "ended" is still a hit with the last vitals)
// belongs inside the cache, so no caller ever inspects a phase.
type Lookup func(hcomidentity.Row) (Result, bool)

// Read is what the CLI (show, list) calls: the socket of the serve behind
// herderstate.Dir() first, then the direct read. The socket step is never a
// wait in the normal path — see herdersock.Ask for the three no-serve cases
// (absent: one stat; stale: one refused dial, file unlinked; hung serve: the
// only bounded wait, herdersock.ClientBudget).
func Read(row hcomidentity.Row) (Result, error) {
	return ReadWith(SocketLookup, row)
}

// ReadWith is the single cache-then-direct body shared by the CLI (cache =
// SocketLookup) and the serve (cache = the observer's Lookup). A nil cache
// means direct only.
func ReadWith(cache Lookup, row hcomidentity.Row) (Result, error) {
	if cache != nil {
		if result, ok := cache(row); ok {
			result.Source = herdersock.SourceCache
			return result, nil
		}
	}
	return ReadDirect(row)
}

// SocketLookup asks the serve behind herderstate.Dir() over herdersock.
func SocketLookup(row hcomidentity.Row) (Result, bool) {
	if row.Tool != "claude" && row.Tool != "codex" || row.SessionID == "" {
		return Result{}, false
	}
	stateDir, err := herderstate.Dir()
	if err != nil {
		return Result{}, false
	}
	response, ok := herdersock.Ask(stateDir, herdersock.Request{Op: herdersock.OpVitals, Tool: row.Tool, Session: row.SessionID, AgentID: row.AgentID})
	if !ok {
		return Result{}, false
	}
	return Result{Vitals: response.Vitals, Path: response.Path, ObservedAt: response.ObservedAt}, true
}

// ReadDirect is the on-demand reverse transcript scan: resolve the path, then
// the tool's ReadVitals. The observer's seed uses the same functions (Seed).
func ReadDirect(row hcomidentity.Row) (Result, error) {
	result := Result{Source: herdersock.SourceDirect}
	if row.Tool != "claude" && row.Tool != "codex" {
		return result, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return result, err
	}
	path, err := ResolvePath(home, row)
	if err != nil {
		if IsResolveRefusal(err) {
			return result, nil
		}
		return result, err
	}
	result.Path = path
	info, err := os.Stat(path)
	if err != nil {
		return result, err
	}
	result.ObservedAt = info.ModTime()
	result.Vitals, _, err = readVitals(row.Tool, IsSubagent(row), path)
	return result, err
}

// IsResolveRefusal reports a typed "no such session file" refusal from either
// tool's resolver, which the CLI shows as empty vitals rather than an error.
func IsResolveRefusal(err error) bool {
	var claudeResolve *claudesession.ResolveError
	var codexResolve *codexsession.ResolveError
	return errors.As(err, &claudeResolve) || errors.As(err, &codexResolve)
}

// ResolvePath is the single tool-aware transcript path resolver.
func ResolvePath(home string, row hcomidentity.Row) (string, error) {
	switch row.Tool {
	case "claude":
		if IsSubagent(row) {
			return claudesession.ResolveSubagent(home, row)
		}
		return claudesession.Resolve(home, row)
	case "codex":
		return codexsession.Resolve(home, row)
	default:
		// Preserve the existing non-file-tool refusal category.
		return claudesession.Resolve(home, row)
	}
}

// Kilo formats token counts with the same nearest-thousand rounding as the web.
func Kilo(value int64) string {
	if value < 1000 {
		return fmt.Sprintf("%d", value)
	}
	return fmt.Sprintf("%dk", (value+500)/1000)
}

// IsSubagent reports a Claude Task transcript row (agent_id set).
func IsSubagent(row hcomidentity.Row) bool {
	return row.Tool == "claude" && row.AgentID != ""
}

// readVitals is the tool dispatch for the reverse scan; the int64 is the
// complete-record end that scan captured.
func readVitals(tool string, subagent bool, path string) (claudesession.Vitals, int64, error) {
	switch {
	case tool == "codex":
		return codexsession.ReadVitals(path)
	case subagent:
		return claudesession.ReadSubagentVitals(path)
	default:
		return claudesession.ReadVitals(path)
	}
}
