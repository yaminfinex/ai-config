// Package gitapi implements bounded, root-contained, read-only Git queries.
package gitapi

import (
	"errors"
	"time"

	"ai-config/tools/herder/internal/fileapi"
)

const (
	SoftCap     = fileapi.SoftCap
	HardCap     = fileapi.HardCap
	LogPageSize = 50
)

var (
	ErrBadRequest  = errors.New("bad git request")
	ErrNotFound    = errors.New("git object not found")
	ErrRefused     = errors.New("git request refused")
	ErrUnavailable = errors.New("git fact unavailable")
)

type Unavailable struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type BranchBase struct {
	Status             string `json:"status"`
	DefaultRef         string `json:"default_ref,omitempty"`
	DefaultSHA         string `json:"default_sha,omitempty"`
	MergeBase          string `json:"merge_base,omitempty"`
	CommitsAheadOfBase *int   `json:"commits_ahead_of_base,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type Repository struct {
	Branch     string     `json:"branch,omitempty"`
	Head       string     `json:"head,omitempty"`
	Upstream   string     `json:"upstream,omitempty"`
	Ahead      *int       `json:"ahead,omitempty"`
	Behind     *int       `json:"behind,omitempty"`
	BranchBase BranchBase `json:"branch_base"`
}

type StatusEntry struct {
	Path         string `json:"path"`
	Kind         string `json:"kind"`
	OldPath      string `json:"old_path,omitempty"`
	Staged       bool   `json:"staged"`
	Unstaged     bool   `json:"unstaged"`
	IndexKind    string `json:"index_kind,omitempty"`
	WorktreeKind string `json:"worktree_kind,omitempty"`
	Additions    *int   `json:"additions,omitempty"`
	Deletions    *int   `json:"deletions,omitempty"`
	Binary       *bool  `json:"binary,omitempty"`
}

type StatusResult struct {
	Root      string         `json:"root"`
	Repo      *Repository    `json:"repo,omitempty"`
	Entries   *[]StatusEntry `json:"entries,omitempty"`
	Git       *Unavailable   `json:"git,omitempty"`
	FetchedAt time.Time      `json:"fetched_at"`
}

type DiffBase struct {
	Kind       string `json:"kind"`
	SHA        string `json:"sha"`
	DefaultRef string `json:"default_ref,omitempty"`
	Label      string `json:"label"`
}

type DiffFacts struct {
	Kind    string `json:"kind"`
	OldPath string `json:"old_path,omitempty"`
	Binary  bool   `json:"binary"`
	OldMode string `json:"old_mode,omitempty"`
	NewMode string `json:"new_mode,omitempty"`
}

type DiffStats struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

type DiffResult struct {
	Root       string     `json:"root"`
	Path       string     `json:"path"`
	Base       DiffBase   `json:"base"`
	Facts      DiffFacts  `json:"facts"`
	Stats      *DiffStats `json:"stats,omitempty"`
	Patch      string     `json:"patch"`
	PatchBytes int64      `json:"patch_bytes"`
	Truncated  bool       `json:"truncated"`
	FetchedAt  *time.Time `json:"fetched_at,omitempty"`
}

type LogEntry struct {
	SHA      string `json:"sha"`
	Author   string `json:"author"`
	Date     string `json:"date"`
	Subject  string `json:"subject"`
	PathThen string `json:"path_then"`
}

type LogResult struct {
	Root             string     `json:"root"`
	Path             string     `json:"path"`
	Entries          []LogEntry `json:"entries"`
	NextCursor       string     `json:"next_cursor,omitempty"`
	HistoryTruncated bool       `json:"history_truncated,omitempty"`
	FetchedAt        time.Time  `json:"fetched_at"`
}

type FileResult struct {
	Root      string  `json:"root"`
	Path      string  `json:"path"`
	SHA       string  `json:"sha"`
	Content   *string `json:"content,omitempty"`
	Binary    bool    `json:"binary"`
	Size      int64   `json:"size"`
	Truncated *bool   `json:"truncated,omitempty"`
}
