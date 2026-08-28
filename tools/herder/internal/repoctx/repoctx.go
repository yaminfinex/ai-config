// Package repoctx derives live, read-only repository facts without guessing.
package repoctx

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var commandTimeout = 5 * time.Second

type Context struct {
	CWD string `json:"cwd"`
	Git *Git   `json:"git,omitempty"`
}

type Git struct {
	Branch     string `json:"branch,omitempty"`
	RemoteURL  string `json:"remote_url,omitempty"`
	WorktreeOf string `json:"worktree_of,omitempty"`
}

// Read reports only facts Git can prove from cwd. A non-repository is a valid
// context with Git omitted.
func Read(ctx context.Context, cwd string) (Context, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return Context{}, fmt.Errorf("make cwd absolute %q: %w", cwd, err)
	}
	absolute = filepath.Clean(absolute)
	result := Context{CWD: absolute}

	paths, err := repositoryPaths(ctx, absolute)
	if err != nil {
		// Repository enrichment is optional. A missing, unhealthy, or slow Git
		// executable must not take down the board carrying this cwd.
		return result, nil
	}
	git := &Git{}
	gitDir, commonDir := filepath.Clean(paths[1]), filepath.Clean(paths[2])
	if gitDir != commonDir && filepath.Base(commonDir) == ".git" {
		git.WorktreeOf = filepath.Dir(commonDir)
	}
	if branch, branchErr := gitOutput(ctx, absolute, "symbolic-ref", "--quiet", "--short", "HEAD"); branchErr == nil {
		git.Branch = strings.TrimSpace(branch)
	} else if ctx.Err() != nil {
		return result, nil
	}
	if remote, remoteErr := gitOutput(ctx, absolute, "remote", "get-url", "origin"); remoteErr == nil {
		git.RemoteURL = strings.TrimSpace(remote)
	} else if ctx.Err() != nil {
		return result, nil
	}
	result.Git = git
	return result, nil
}

// LinkedWorktree reports whether cwd is proven to live in a linked worktree.
// A non-repository is not an error.
func LinkedWorktree(ctx context.Context, cwd string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	paths, err := repositoryPaths(ctx, cwd)
	if err != nil {
		return false, nil
	}
	return filepath.Clean(paths[1]) != filepath.Clean(paths[2]), nil
}

func repositoryPaths(ctx context.Context, cwd string) ([]string, error) {
	output, err := gitOutput(ctx, cwd, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	lines := nonemptyLines(output)
	if len(lines) != 3 {
		return nil, fmt.Errorf("git rev-parse in %q returned %d paths, want 3", cwd, len(lines))
	}
	return lines, nil
}

func gitOutput(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s in %q: %w: %s", strings.Join(args, " "), cwd, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func nonemptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
