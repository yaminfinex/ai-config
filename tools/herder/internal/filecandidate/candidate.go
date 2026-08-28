// Package filecandidate defines typed entries in the derived quick-open index.
package filecandidate

// Kind distinguishes readable files from their derived ancestor directories.
type Kind string

const (
	KindFile Kind = "file"
	KindDir  Kind = "dir"
)

// Candidate is one root-relative quick-open entry.
type Candidate struct {
	Path string
	Kind Kind
}
