package gitapi

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func ReadStatus(ctx context.Context, root string, now func() time.Time) (StatusResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	fetched := now()
	loc, err := discover(ctx, root)
	if err != nil {
		return StatusResult{Root: root, Git: &Unavailable{Status: "unavailable", Reason: repositoryUnavailableReason(err)}, FetchedAt: fetched}, nil
	}
	out, err := gitOutput(ctx, loc.repoTop, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", "--", pathspec(loc))
	if err != nil {
		return StatusResult{Root: loc.root, Git: &Unavailable{Status: "unavailable", Reason: compactReason(err)}, FetchedAt: fetched}, nil
	}
	repo, entries, err := parseStatus(loc, out)
	if err != nil {
		return StatusResult{Root: loc.root, Git: &Unavailable{Status: "unavailable", Reason: err.Error()}, FetchedAt: fetched}, nil
	}
	repo.BranchBase = proveBranchBase(ctx, loc.repoTop)
	attachStatusStats(ctx, loc, entries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return StatusResult{Root: loc.root, Repo: &repo, Entries: &entries, FetchedAt: fetched}, nil
}

func parseStatus(loc location, output []byte) (Repository, []StatusEntry, error) {
	records := bytes.Split(output, []byte{0})
	repo := Repository{}
	entries := make([]StatusEntry, 0)
	for index := 0; index < len(records); index++ {
		record := string(records[index])
		if record == "" {
			continue
		}
		switch record[0] {
		case '#':
			if err := parseBranchHeader(&repo, record); err != nil {
				return Repository{}, nil, err
			}
		case '1':
			fields := strings.SplitN(record, " ", 9)
			if len(fields) != 9 || len(fields[1]) != 2 {
				return Repository{}, nil, fmt.Errorf("unrecognized porcelain-v2 ordinary record %q", record)
			}
			if !validXY(fields[1]) {
				return Repository{}, nil, fmt.Errorf("unrecognized porcelain-v2 XY status %q", fields[1])
			}
			path, ok := publicPath(loc, fields[8])
			if !ok {
				continue
			}
			entries = append(entries, entryFromXY(path, "", fields[1], false))
		case '2':
			fields := strings.SplitN(record, " ", 10)
			if len(fields) != 10 || len(fields[1]) != 2 || index+1 >= len(records) {
				return Repository{}, nil, fmt.Errorf("unrecognized porcelain-v2 rename record %q", record)
			}
			if !validXY(fields[1]) {
				return Repository{}, nil, fmt.Errorf("unrecognized porcelain-v2 XY status %q", fields[1])
			}
			index++
			path, ok := publicPath(loc, fields[9])
			if !ok {
				continue
			}
			oldPath, _ := publicPath(loc, string(records[index]))
			entries = append(entries, entryFromXY(path, oldPath, fields[1], false))
		case 'u':
			fields := strings.SplitN(record, " ", 11)
			if len(fields) != 11 || len(fields[1]) != 2 {
				return Repository{}, nil, fmt.Errorf("unrecognized porcelain-v2 conflict record %q", record)
			}
			if !validXY(fields[1]) {
				return Repository{}, nil, fmt.Errorf("unrecognized porcelain-v2 XY status %q", fields[1])
			}
			path, ok := publicPath(loc, fields[10])
			if ok {
				entry := entryFromXY(path, "", fields[1], true)
				entries = append(entries, entry)
			}
		case '?':
			if !strings.HasPrefix(record, "? ") {
				return Repository{}, nil, fmt.Errorf("unrecognized porcelain-v2 untracked record %q", record)
			}
			path, ok := publicPath(loc, record[2:])
			if ok {
				entries = append(entries, StatusEntry{Path: path, Kind: "untracked", Unstaged: true})
			}
		case '!':
			continue
		default:
			return Repository{}, nil, fmt.Errorf("unrecognized porcelain-v2 record %q", record)
		}
	}
	return repo, entries, nil
}

func parseBranchHeader(repo *Repository, record string) error {
	fields := strings.Split(record, " ")
	if len(fields) < 3 || fields[0] != "#" {
		return fmt.Errorf("unrecognized porcelain-v2 branch header %q", record)
	}
	value := strings.Join(fields[2:], " ")
	switch fields[1] {
	case "branch.oid":
		if value != "(initial)" {
			repo.Head = value
		}
	case "branch.head":
		if value != "(detached)" {
			repo.Branch = value
		}
	case "branch.upstream":
		repo.Upstream = value
	case "branch.ab":
		if len(fields) != 4 || !strings.HasPrefix(fields[2], "+") || !strings.HasPrefix(fields[3], "-") {
			return fmt.Errorf("unrecognized porcelain-v2 ahead/behind header %q", record)
		}
		ahead, err1 := strconv.Atoi(strings.TrimPrefix(fields[2], "+"))
		behind, err2 := strconv.Atoi(strings.TrimPrefix(fields[3], "-"))
		if err1 != nil || err2 != nil {
			return fmt.Errorf("unrecognized porcelain-v2 ahead/behind header %q", record)
		}
		repo.Ahead, repo.Behind = &ahead, &behind
	default:
		return fmt.Errorf("unrecognized porcelain-v2 branch header %q", record)
	}
	return nil
}

func entryFromXY(path, oldPath, xy string, conflicted bool) StatusEntry {
	indexKind := statusKind(xy[0])
	worktreeKind := statusKind(xy[1])
	entry := StatusEntry{
		Path: path, OldPath: oldPath, Staged: indexKind != "", Unstaged: worktreeKind != "",
		IndexKind: indexKind, WorktreeKind: worktreeKind,
	}
	if conflicted {
		entry.Kind = "conflicted"
	} else if indexKind == "renamed" || worktreeKind == "renamed" {
		entry.Kind = "renamed"
	} else if indexKind == "copied" || worktreeKind == "copied" {
		entry.Kind = "copied"
	} else if worktreeKind != "" {
		entry.Kind = worktreeKind
	} else {
		entry.Kind = indexKind
	}
	return entry
}

func statusKind(value byte) string {
	switch value {
	case '.', ' ':
		return ""
	case 'M':
		return "modified"
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'T':
		return "type_changed"
	case 'U':
		return "conflicted"
	default:
		return "unknown"
	}
}

func validXY(value string) bool {
	return len(value) == 2 && statusKind(value[0]) != "unknown" && statusKind(value[1]) != "unknown"
}

func proveBranchBase(ctx context.Context, cwd string) BranchBase {
	refOut, err := gitOutput(ctx, cwd, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return BranchBase{Status: "unavailable", Reason: "origin/HEAD is not configured"}
	}
	ref := strings.TrimSpace(string(refOut))
	defaultOut, err := gitOutput(ctx, cwd, "rev-parse", "--verify", "--end-of-options", "refs/remotes/origin/HEAD^{commit}")
	if err != nil {
		return BranchBase{Status: "unavailable", Reason: "origin/HEAD does not resolve to a commit"}
	}
	mergeOut, err := gitOutput(ctx, cwd, "merge-base", "HEAD", "refs/remotes/origin/HEAD")
	if err != nil || len(strings.Fields(string(mergeOut))) != 1 {
		return BranchBase{Status: "unavailable", Reason: "merge-base with origin/HEAD cannot be proved"}
	}
	return BranchBase{Status: "available", DefaultRef: ref, DefaultSHA: strings.TrimSpace(string(defaultOut)), MergeBase: strings.TrimSpace(string(mergeOut))}
}

func attachStatusStats(ctx context.Context, loc location, entries []StatusEntry) {
	out, err := gitOutput(ctx, loc.repoTop, "diff", "--numstat", "-z", "--find-renames", "--find-copies-harder", "HEAD", "--", pathspec(loc))
	if err != nil {
		return
	}
	stats, err := parseNumstat(loc, out)
	if err != nil {
		return
	}
	for index := range entries {
		if stat, ok := stats[entries[index].Path]; ok {
			entries[index].Additions = &stat.Additions
			entries[index].Deletions = &stat.Deletions
			binary := stat.Binary
			entries[index].Binary = &binary
		}
	}
}

func compactReason(err error) string {
	text := err.Error()
	if len(text) > 4096 {
		return text[:4096] + "... [truncated]"
	}
	return text
}

func repositoryUnavailableReason(err error) string {
	if strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
		return "not a git repository"
	}
	return compactReason(err)
}
