// Package refs parses bottle references of the form name[@version].
//
// It is pure parsing: it knows nothing about the registry or any store.
// Resolution of a Ref against actual bottles lives in the store package.
package refs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// NamePattern is the regex a bottle name must match in full. The @ character
// is reserved as the name/version separator and can never appear in a name.
const NamePattern = `[a-z0-9][a-z0-9-]*`

var nameRe = regexp.MustCompile(`^` + NamePattern + `$`)

// Ref is a parsed bottle reference: a name plus an optional pinned version.
// Version 0 means unpinned ("resolve to the latest version").
type Ref struct {
	Name    string
	Version int
}

// Pinned reports whether the reference names a specific version.
func (r Ref) Pinned() bool { return r.Version > 0 }

// String renders the reference back to its name[@version] form.
func (r Ref) String() string {
	if r.Pinned() {
		return fmt.Sprintf("%s@%d", r.Name, r.Version)
	}
	return r.Name
}

// Parse parses a name[@version] reference. The name must match NamePattern;
// the version, when present, must be a positive integer.
func Parse(s string) (Ref, error) {
	name, ver, pinned := strings.Cut(s, "@")
	if err := ValidateName(name); err != nil {
		return Ref{}, err
	}
	if !pinned {
		return Ref{Name: name}, nil
	}
	n, err := strconv.Atoi(ver)
	if err != nil || n < 1 {
		return Ref{}, fmt.Errorf("invalid version %q in %q: must be a positive integer", ver, s)
	}
	return Ref{Name: name, Version: n}, nil
}

// ValidateName checks a bare bottle name (no version suffix) against
// NamePattern.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid name %q: must match %s (@ is reserved for version refs)", name, NamePattern)
	}
	return nil
}
