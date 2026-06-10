package store

import "time"

// Source records where a bottle came from: the original session and the
// repo state at bottle time. Field names follow the origin spec; recorded
// for display and provenance, never restored.
type Source struct {
	SessionID  string `json:"session_id"`
	Harness    string `json:"harness"`
	CWD        string `json:"cwd"`
	GitBranch  string `json:"git_branch,omitempty"`
	GitSHA     string `json:"git_sha,omitempty"`
	CutTurn    int    `json:"cut_turn"`
	TotalTurns int    `json:"total_turns"`
}

// Parent links a rebottled bottle to its parent: the parent bottle id and
// the decant session that sat between them. Absent for root bottles.
type Parent struct {
	BottleID        string `json:"bottle_id"`
	DecantSessionID string `json:"decant_session_id"`
}

// Meta is a bottle's meta.json. Bottles are immutable after create with two
// sanctioned exceptions: Note (via `bottle note`) and Name/PreviousNames
// (via `bottle rename`, so log lineage stays continuous across renames).
type Meta struct {
	Name          string    `json:"name"`
	Version       int       `json:"version"`
	Created       time.Time `json:"created"`
	Note          string    `json:"note"`
	PreviousNames []string  `json:"previous_names,omitempty"`
	Source        Source    `json:"source"`
	Parent        *Parent   `json:"parent,omitempty"`

	// InheritedLines is the parent transcript length (in lines) at decant
	// time; zero for root bottles.
	InheritedLines int `json:"inherited_lines"`
	// Compacted marks a transcript containing compact boundaries.
	Compacted bool `json:"compacted"`
	// CompactionReachesInherited marks a compact boundary whose logical
	// parent reaches into inherited context (compaction swallowed context
	// from before this lineage started).
	CompactionReachesInherited bool `json:"compaction_reaches_inherited"`
	// RewoundIntoParent marks a bottle cut at a turn at or before the
	// inherited prefix, making it effectively a prefix of its parent.
	RewoundIntoParent bool `json:"rewound_into_parent"`
}
