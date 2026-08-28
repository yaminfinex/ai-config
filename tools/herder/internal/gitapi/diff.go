package gitapi

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type numstatRecord struct {
	Additions int
	Deletions int
	Binary    bool
}

type rawRecord struct {
	oldPath string
	kind    string
	oldMode string
	newMode string
}

func ReadDiff(ctx context.Context, root, requestedPath, baseKind string, now func() time.Time) (DiffResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	loc, err := locatePath(ctx, root, requestedPath)
	if err != nil {
		return DiffResult{}, err
	}
	base, err := resolveDiffBase(ctx, loc, baseKind)
	if err != nil {
		return DiffResult{}, err
	}
	current, historical, err := pathAvailability(ctx, loc, base.SHA)
	if err != nil {
		return DiffResult{}, err
	}
	if !current && !historical {
		return DiffResult{}, fmt.Errorf("%w: path %q does not exist now or at base %s", ErrNotFound, requestedPath, base.SHA)
	}
	rawOut, err := gitOutput(ctx, loc.repoTop, "diff", "--raw", "-z", "--find-renames", "--find-copies-harder", base.SHA, "--", pathspec(loc))
	if err != nil {
		return DiffResult{}, err
	}
	raw, err := parseRaw(loc, rawOut)
	if err != nil {
		return DiffResult{}, err
	}
	record := raw[loc.path]
	untracked := record == nil && current && !historical
	facts := DiffFacts{Kind: "unchanged"}
	patchPaths := []string{loc.repoPath}
	if record != nil {
		facts.Kind = record.kind
		facts.OldPath = record.oldPath
		if record.oldMode != record.newMode {
			facts.OldMode, facts.NewMode = record.oldMode, record.newMode
		}
		if record.oldPath != "" {
			oldRepoPath := record.oldPath
			if loc.rootPrefix != "." {
				oldRepoPath = strings.TrimSuffix(filepath.ToSlash(loc.rootPrefix), "/") + "/" + record.oldPath
			}
			patchPaths = []string{oldRepoPath, loc.repoPath}
		}
	}
	var numOut []byte
	if untracked {
		numOut, err = gitOutputAllowExitOne(ctx, loc.repoTop, "diff", "--no-index", "--numstat", "-z", "--", "/dev/null", loc.repoPath)
		if err != nil {
			return DiffResult{}, err
		}
		facts.Kind = "added"
		facts.OldMode = "000000"
		facts.NewMode, err = workingTreeMode(filepath.Join(loc.root, filepath.FromSlash(loc.path)))
	} else {
		numOut, err = gitOutput(ctx, loc.repoTop, "diff", "--numstat", "-z", "--find-renames", "--find-copies-harder", base.SHA, "--", pathspec(loc))
	}
	if err != nil {
		return DiffResult{}, err
	}
	numstats, err := parseNumstat(loc, numOut)
	if err != nil {
		return DiffResult{}, err
	}
	var stats *DiffStats
	if stat, ok := numstats[loc.path]; ok {
		facts.Binary = stat.Binary
		if !stat.Binary {
			stats = &DiffStats{Additions: stat.Additions, Deletions: stat.Deletions}
		}
	} else if facts.Kind == "unchanged" {
		stats = &DiffStats{}
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--find-renames", "--find-copies-harder", "--src-prefix=a/", "--dst-prefix=b/", base.SHA, "--"}
	allowExitOne := false
	if untracked {
		args = []string{"diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "--src-prefix=a/", "--dst-prefix=b/", "--", "/dev/null", loc.repoPath}
		allowExitOne = true
	} else {
		args = append(args, patchPaths...)
	}
	patch, total, truncated, err := cappedGitOutput(ctx, loc.repoTop, allowExitOne, args...)
	if err != nil {
		return DiffResult{}, err
	}
	return DiffResult{
		Root: loc.root, Path: loc.path, Base: base, Facts: facts, Stats: stats,
		Patch: patch, PatchBytes: total, Truncated: truncated, FetchedAt: now(),
	}, nil
}

func resolveDiffBase(ctx context.Context, loc location, kind string) (DiffBase, error) {
	switch kind {
	case "uncommitted":
		out, err := gitOutput(ctx, loc.repoTop, "rev-parse", "--verify", "--end-of-options", "HEAD^{commit}")
		if err != nil {
			return DiffBase{}, fmt.Errorf("%w: HEAD does not resolve to a commit", ErrUnavailable)
		}
		return DiffBase{Kind: kind, SHA: strings.TrimSpace(string(out)), Label: "HEAD"}, nil
	case "branch":
		proof := proveBranchBase(ctx, loc.repoTop)
		if proof.Status != "available" {
			return DiffBase{}, fmt.Errorf("%w: %s", ErrUnavailable, proof.Reason)
		}
		return DiffBase{
			Kind: kind, SHA: proof.MergeBase, DefaultRef: proof.DefaultRef,
			Label: "merge-base with " + proof.DefaultRef + "; includes committed and uncommitted work",
		}, nil
	default:
		return DiffBase{}, fmt.Errorf("%w: base must be exactly uncommitted or branch", ErrRefused)
	}
}

func pathAvailability(ctx context.Context, loc location, base string) (bool, bool, error) {
	current := false
	if info, err := os.Lstat(filepath.Join(loc.root, filepath.FromSlash(loc.path))); err == nil {
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return false, false, fmt.Errorf("%w: path %q is not a file", ErrRefused, loc.path)
		}
		current = true
	} else if !os.IsNotExist(err) {
		return false, false, fmt.Errorf("inspect path %q: %w", loc.path, err)
	}
	typeOut, err := gitOutput(ctx, loc.repoTop, "cat-file", "-t", base+":"+loc.repoPath)
	if err != nil {
		if ctx.Err() != nil || !missingPathError(err) {
			return false, false, err
		}
		return current, false, nil
	}
	if strings.TrimSpace(string(typeOut)) != "blob" {
		return false, false, fmt.Errorf("%w: path %q is not a file at base %s", ErrRefused, loc.path, base)
	}
	return current, true, nil
}

func workingTreeMode(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect untracked path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "120000", nil
	}
	if info.Mode()&0o111 != 0 {
		return "100755", nil
	}
	return "100644", nil
}

func parseRaw(loc location, output []byte) (map[string]*rawRecord, error) {
	records := bytes.Split(output, []byte{0})
	result := make(map[string]*rawRecord)
	for index := 0; index < len(records); {
		if len(records[index]) == 0 {
			index++
			continue
		}
		header := strings.Fields(string(records[index]))
		index++
		if len(header) != 5 || !strings.HasPrefix(header[0], ":") || index >= len(records) {
			return nil, fmt.Errorf("unrecognized git diff --raw record %q", records[index-1])
		}
		status := header[4]
		if status == "" {
			return nil, fmt.Errorf("empty git diff --raw status")
		}
		kind, ok := rawKind(status[0])
		if !ok {
			return nil, fmt.Errorf("unrecognized git diff --raw status %q", status)
		}
		first := string(records[index])
		index++
		oldRepo, newRepo := "", first
		if status[0] == 'R' || status[0] == 'C' {
			if index >= len(records) {
				return nil, fmt.Errorf("git diff --raw rename is missing destination")
			}
			oldRepo, newRepo = first, string(records[index])
			index++
		}
		path, inRoot := publicPath(loc, newRepo)
		if !inRoot {
			continue
		}
		oldPath := ""
		if oldRepo != "" {
			oldPath, _ = publicPath(loc, oldRepo)
		}
		result[path] = &rawRecord{oldPath: oldPath, kind: kind, oldMode: strings.TrimPrefix(header[0], ":"), newMode: header[1]}
	}
	return result, nil
}

func rawKind(status byte) (string, bool) {
	switch status {
	case 'M':
		return "modified", true
	case 'A':
		return "added", true
	case 'D':
		return "deleted", true
	case 'R':
		return "renamed", true
	case 'C':
		return "copied", true
	case 'T':
		return "type_changed", true
	default:
		return "", false
	}
}

func parseNumstat(loc location, output []byte) (map[string]numstatRecord, error) {
	records := bytes.Split(output, []byte{0})
	result := make(map[string]numstatRecord)
	for index := 0; index < len(records); index++ {
		if len(records[index]) == 0 {
			continue
		}
		fields := bytes.SplitN(records[index], []byte{'\t'}, 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unrecognized git diff --numstat record %q", records[index])
		}
		path := string(fields[2])
		if path == "" {
			if index+2 >= len(records) {
				return nil, fmt.Errorf("git diff --numstat rename is missing paths")
			}
			index += 2
			path = string(records[index])
		}
		public, ok := publicPath(loc, path)
		if !ok {
			continue
		}
		record := numstatRecord{}
		if string(fields[0]) == "-" && string(fields[1]) == "-" {
			record.Binary = true
		} else {
			additions, err1 := strconv.Atoi(string(fields[0]))
			deletions, err2 := strconv.Atoi(string(fields[1]))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("unrecognized git diff --numstat counts %q", records[index])
			}
			record.Additions, record.Deletions = additions, deletions
		}
		result[public] = record
	}
	return result, nil
}

func cappedGitOutput(ctx context.Context, cwd string, allowExitOne bool, args ...string) (string, int64, bool, error) {
	retained, total, hard, err := gitStream(ctx, cwd, SoftCap, HardCap, allowExitOne, args...)
	if err != nil {
		return "", total, false, err
	}
	if hard {
		return "", total, false, fmt.Errorf("%w: diff output exceeds 4 MiB; patches above 4 MiB are not served", ErrRefused)
	}
	if !utf8.Valid(retained) && total > SoftCap {
		for removed := 0; removed < utf8.UTFMax-1 && len(retained) > 0 && !utf8.Valid(retained); removed++ {
			retained = retained[:len(retained)-1]
		}
	}
	if !utf8.Valid(retained) {
		return "", total, false, fmt.Errorf("git diff output is not UTF-8")
	}
	return string(retained), total, total > SoftCap, nil
}
