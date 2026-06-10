package claude

import (
	"fmt"
	"os"
	"path/filepath"

	"ai-config/tools/bottle/internal/transcript"
)

// MaterializeRequest describes a decant seed to write.
type MaterializeRequest struct {
	SourcePath   string // the bottle's frozen transcript.jsonl
	ProjectsRoot string // typically $HOME/.claude/projects
	Cwd          string // the run cwd whose encoded dir the seed lands in
}

// MaterializeResult reports what Materialize produced.
type MaterializeResult struct {
	SessionID         string // fresh session id rewritten into the seed
	SeedPath          string // absolute path of the written seed file
	ProjectDir        string // the encoded project directory it lives in
	CompactBoundaries int    // compact_boundary count (for create-time warnings)
}

// Materialize writes a fresh, resumable seed session from a bottle transcript:
// it rewrites the sessionId on every line (transcript.Rewrite) into a new
// file at <projectsRoot>/<encoded-cwd>/<new-id>.jsonl.
//
// The source is validated (parsed) BEFORE anything is written, so a GC'd or
// unreadable source fails cleanly with nothing left on disk. The encoded
// project directory is created 0700 when absent. The seed is written via a
// temp file + rename and linted before the rename, so a partial or malformed
// seed never becomes visible.
func Materialize(req MaterializeRequest) (MaterializeResult, error) {
	// 1. Validate the source first — no writes happen if this fails.
	info, err := transcript.IndexFile(req.SourcePath)
	if err != nil {
		return MaterializeResult{}, fmt.Errorf("source session %s: %w", req.SourcePath, err)
	}

	// 2. Fresh session id.
	id, err := transcript.NewSessionID()
	if err != nil {
		return MaterializeResult{}, fmt.Errorf("generate session id: %w", err)
	}

	// 3. Ensure the encoded project directory exists (0700 when created).
	dir := ProjectDir(req.ProjectsRoot, req.Cwd)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return MaterializeResult{}, fmt.Errorf("create project dir %s: %w", dir, err)
	}

	seed := filepath.Join(dir, id+".jsonl")
	if err := rewriteSeed(req.SourcePath, dir, seed, id); err != nil {
		return MaterializeResult{}, err
	}

	return MaterializeResult{
		SessionID:         id,
		SeedPath:          seed,
		ProjectDir:        dir,
		CompactBoundaries: info.CompactBoundaries(),
	}, nil
}

// rewriteSeed streams src through transcript.Rewrite into a temp file in dir,
// lints the result, then atomically renames it to seed. Any failure removes the
// temp file so no partial seed is left behind.
func rewriteSeed(srcPath, dir, seed, id string) (err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source session: %w", err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(dir, ".seed-*.jsonl.tmp")
	if err != nil {
		return fmt.Errorf("create seed temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpName)
		}
	}()

	if err = transcript.Rewrite(src, tmp, id); err != nil {
		tmp.Close()
		return fmt.Errorf("rewrite session id: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("flush seed temp file: %w", err)
	}

	// Insurance: a materialized seed must have no dangling uuid references.
	lintFile, err := os.Open(tmpName)
	if err != nil {
		return fmt.Errorf("reopen seed for lint: %w", err)
	}
	lintErr := transcript.Lint(lintFile)
	lintFile.Close()
	if lintErr != nil {
		err = fmt.Errorf("seed failed lint: %w", lintErr)
		return err
	}

	if err = os.Rename(tmpName, seed); err != nil {
		return fmt.Errorf("place seed file: %w", err)
	}
	return nil
}
