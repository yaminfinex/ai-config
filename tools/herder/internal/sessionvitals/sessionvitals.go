// Package sessionvitals resolves live session transcripts and reads their
// latest model and context facts without persisting those changing values.
package sessionvitals

import (
	"errors"
	"fmt"
	"os"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/codexsession"
	"ai-config/tools/herder/internal/hcomidentity"
)

// Read performs today's on-demand reverse transcript scan. Once the
// observer/daemon exists, this becomes a central context cache read over the
// local socket; the transcript remains the authority behind that cache.
func Read(row hcomidentity.Row) (claudesession.Vitals, string, time.Time, error) {
	if row.Tool != "claude" && row.Tool != "codex" {
		return claudesession.Vitals{}, "", time.Time{}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return claudesession.Vitals{}, "", time.Time{}, err
	}
	path, err := ResolvePath(home, row)
	if err != nil {
		var claudeResolve *claudesession.ResolveError
		var codexResolve *codexsession.ResolveError
		if errors.As(err, &claudeResolve) || errors.As(err, &codexResolve) {
			return claudesession.Vitals{}, "", time.Time{}, nil
		}
		return claudesession.Vitals{}, "", time.Time{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return claudesession.Vitals{}, path, time.Time{}, err
	}
	var vitals claudesession.Vitals
	if row.Tool == "codex" {
		vitals, err = codexsession.ReadVitals(path)
	} else if isSubagent(row) {
		vitals, err = claudesession.ReadSubagentVitals(path)
	} else {
		vitals, err = claudesession.ReadVitals(path)
	}
	return vitals, path, info.ModTime(), err
}

// ResolvePath is the single tool-aware transcript path resolver.
func ResolvePath(home string, row hcomidentity.Row) (string, error) {
	switch row.Tool {
	case "claude":
		if isSubagent(row) {
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

func isSubagent(row hcomidentity.Row) bool {
	return row.Tool == "claude" && row.AgentID != ""
}
