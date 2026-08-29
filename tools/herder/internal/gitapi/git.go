package gitapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ai-config/tools/herder/internal/fileresolver"
)

var commandTimeout = 5 * time.Second

type location struct {
	root       string
	repoTop    string
	rootPrefix string
	path       string
	repoPath   string
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, commandTimeout)
}

func absoluteRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("make root absolute %q: %w", root, err)
	}
	return filepath.Clean(absolute), nil
}

func discover(ctx context.Context, root string) (location, error) {
	absolute, err := absoluteRoot(root)
	if err != nil {
		return location{}, err
	}
	out, err := gitOutput(ctx, absolute, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return location{}, err
	}
	top := filepath.Clean(strings.TrimSpace(string(out)))
	rel, err := filepath.Rel(top, absolute)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return location{}, fmt.Errorf("git top-level %q does not contain root %q", top, absolute)
	}
	return location{root: absolute, repoTop: top, rootPrefix: rel}, nil
}

func locatePath(ctx context.Context, root, requested string) (location, error) {
	loc, err := discover(ctx, root)
	if err != nil {
		return location{}, fmt.Errorf("%w: %s", ErrUnavailable, repositoryUnavailableReason(err))
	}
	relative, err := validateRelative(requested)
	if err != nil {
		return location{}, err
	}
	// Existing paths reuse the shared containment primitive. Historical paths
	// may no longer exist; Git object lookup never follows working-tree links.
	if _, statErr := os.Lstat(filepath.Join(loc.root, relative)); statErr == nil {
		resolved, resolveErr := fileresolver.ResolveWithinRoot(loc.root, relative)
		if resolveErr != nil {
			return location{}, fmt.Errorf("%w: %v", ErrRefused, resolveErr)
		}
		if gitInternal(loc.root, resolved) {
			return location{}, fmt.Errorf("%w: .git internals are not served: %q", ErrRefused, requested)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return location{}, fmt.Errorf("inspect path %q: %w", requested, statErr)
	}
	loc.path = filepath.ToSlash(relative)
	loc.repoPath = filepath.ToSlash(filepath.Clean(filepath.Join(loc.rootPrefix, relative)))
	return loc, nil
}

func validateRelative(requested string) (string, error) {
	if requested == "" || strings.IndexByte(requested, 0) >= 0 || filepath.IsAbs(requested) {
		return "", fmt.Errorf("%w: path must be a non-empty root-relative path: %q", ErrRefused, requested)
	}
	clean := filepath.Clean(filepath.FromSlash(requested))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes root: %q", ErrRefused, requested)
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".git" {
			return "", fmt.Errorf("%w: .git internals are not served: %q", ErrRefused, requested)
		}
	}
	return clean, nil
}

func gitInternal(root, resolved string) bool {
	rel, err := filepath.Rel(filepath.Join(root, ".git"), resolved)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func pathspec(loc location) string {
	if loc.rootPrefix == "." {
		return "."
	}
	return filepath.ToSlash(loc.rootPrefix)
}

func publicPath(loc location, repoPath string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(repoPath))
	if loc.rootPrefix == "." {
		return filepath.ToSlash(clean), true
	}
	rel, err := filepath.Rel(loc.rootPrefix, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func gitOutput(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	return gitOutputWithExit(ctx, cwd, false, args...)
}

func gitOutputAllowExitOne(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	return gitOutputWithExit(ctx, cwd, true, args...)
}

func gitOutputWithExit(ctx context.Context, cwd string, allowExitOne bool, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", gitCommandArgs(cwd, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil && !(allowExitOne && exitCode(err) == 1) {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("git %s in %q: %w: %s", strings.Join(args, " "), cwd, err, detail)
	}
	return stdout.Bytes(), nil
}

func gitStream(ctx context.Context, cwd string, retainLimit, hardLimit int64, allowExitOne bool, args ...string) ([]byte, int64, bool, error) {
	cmd := exec.CommandContext(ctx, "git", gitCommandArgs(cwd, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, false, fmt.Errorf("open git stdout: %w", err)
	}
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, 0, false, fmt.Errorf("start git %s: %w", strings.Join(args, " "), err)
	}
	buffer := make([]byte, 32*1024)
	retained := make([]byte, 0, min(retainLimit, int64(len(buffer))))
	var total int64
	hard := false
	for {
		read, readErr := stdout.Read(buffer)
		if read > 0 {
			total += int64(read)
			if remaining := int(retainLimit) - len(retained); remaining > 0 {
				keep := read
				if keep > remaining {
					keep = remaining
				}
				retained = append(retained, buffer[:keep]...)
			}
			if hardLimit > 0 && total > hardLimit {
				hard = true
				_ = cmd.Process.Kill()
				break
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return nil, total, false, fmt.Errorf("read git output: %w", readErr)
			}
			break
		}
	}
	waitErr := cmd.Wait()
	if waitErr != nil && !hard && !(allowExitOne && exitCode(waitErr) == 1) {
		return nil, total, false, fmt.Errorf("git %s in %q: %w: %s", strings.Join(args, " "), cwd, waitErr, stderr.String())
	}
	return retained, total, hard, nil
}

func gitCommandArgs(cwd string, args ...string) []string {
	command := []string{"-C", cwd, "-c", "core.hooksPath=", "-c", "core.fsmonitor=false"}
	return append(command, args...)
}

type limitedBuffer struct{ bytes.Buffer }

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	if buffer.Len() < 4096 {
		remaining := 4096 - buffer.Len()
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.Buffer.Write(data)
	}
	return original, nil
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func fullSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

func missingPathError(err error) bool {
	text := err.Error()
	return strings.Contains(text, "does not exist in") || strings.Contains(text, "exists on disk, but not in")
}
