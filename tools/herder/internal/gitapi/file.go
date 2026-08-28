package gitapi

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

func ReadFile(ctx context.Context, root, requestedPath, sha string) (FileResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	loc, err := locatePath(ctx, root, requestedPath)
	if err != nil {
		return FileResult{}, err
	}
	if !fullSHA(sha) {
		return FileResult{}, fmt.Errorf("%w: invalid commit sha %q", ErrNotFound, sha)
	}
	commitOut, err := gitOutput(ctx, loc.repoTop, "rev-parse", "--verify", "--end-of-options", sha+"^{commit}")
	if err != nil {
		return FileResult{}, fmt.Errorf("%w: unknown commit %q", ErrNotFound, sha)
	}
	commit := strings.TrimSpace(string(commitOut))
	spec := commit + ":" + loc.repoPath
	typeOut, err := gitOutput(ctx, loc.repoTop, "cat-file", "-t", spec)
	if err != nil {
		if ctx.Err() != nil || !missingPathError(err) {
			return FileResult{}, err
		}
		return FileResult{}, fmt.Errorf("%w: path %q is absent at commit %s", ErrNotFound, requestedPath, commit)
	}
	if strings.TrimSpace(string(typeOut)) != "blob" {
		return FileResult{}, fmt.Errorf("%w: path %q is not a file at commit %s", ErrRefused, requestedPath, commit)
	}
	sizeOut, err := gitOutput(ctx, loc.repoTop, "cat-file", "-s", spec)
	if err != nil {
		return FileResult{}, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64)
	if err != nil || size < 0 {
		return FileResult{}, fmt.Errorf("git cat-file returned invalid blob size %q", sizeOut)
	}
	if size > HardCap {
		return FileResult{}, fmt.Errorf("%w: blob %q is %d bytes; files above 4 MiB are not served", ErrRefused, requestedPath, size)
	}
	content, total, hard, err := gitStream(ctx, loc.repoTop, SoftCap+1, HardCap, false, "show", spec)
	if err != nil {
		return FileResult{}, err
	}
	if hard {
		return FileResult{}, fmt.Errorf("%w: blob %q exceeds 4 MiB", ErrRefused, requestedPath)
	}
	if total != size {
		return FileResult{}, fmt.Errorf("git show returned %d bytes for %d-byte blob %q", total, size, requestedPath)
	}
	result := FileResult{Root: loc.root, Path: loc.path, SHA: commit, Size: size}
	probe := content
	truncated := int64(len(probe)) > SoftCap
	if truncated {
		probe = probe[:SoftCap]
		for len(probe) > 0 && !utf8.Valid(probe) {
			probe = probe[:len(probe)-1]
		}
	}
	if bytes.IndexByte(probe, 0) >= 0 || !utf8.Valid(probe) {
		result.Binary = true
		return result, nil
	}
	text := string(probe)
	result.Content = &text
	result.Truncated = &truncated
	return result, nil
}
