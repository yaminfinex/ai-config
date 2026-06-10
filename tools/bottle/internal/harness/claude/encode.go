package claude

import "path/filepath"

// EncodeProjectDir derives the on-disk project directory name Claude Code uses
// for a working directory: every byte that is not ASCII alphanumeric becomes a
// '-'. This mirrors the harness's own scheme (verified against real
// ~/.claude/projects/ names, e.g. "/home/ubuntu/.herdr/worktrees/ai-config" →
// "-home-ubuntu--herdr-worktrees-ai-config").
//
// The mapping is lossy (distinct cwds can collide) and MUST NOT be inverted —
// always derive the encoded name from a known cwd, never reconstruct a cwd from
// a directory name. Runs of non-alphanumerics are preserved one-for-one (no
// collapsing), matching `tr -c 'A-Za-z0-9' '-'`.
func EncodeProjectDir(cwd string) string {
	b := []byte(cwd)
	for i := range b {
		c := b[i]
		alnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !alnum {
			b[i] = '-'
		}
	}
	return string(b)
}

// ProjectDir returns the absolute path to the encoded project directory for cwd
// under projectsRoot (typically $HOME/.claude/projects).
func ProjectDir(projectsRoot, cwd string) string {
	return filepath.Join(projectsRoot, EncodeProjectDir(cwd))
}
