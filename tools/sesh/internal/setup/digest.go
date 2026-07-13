package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DP-4b provenance digest: every config file setup writes ends with a comment
// line carrying the SHA-256 of the content above it. An intact digest proves
// the file is byte-for-byte what setup wrote — including an operator edit
// that only changed the URL value, which shape-comparison could never catch —
// so a later setup run may replace it without --force. A broken or absent
// digest means operator-owned (or pre-digest legacy) content and re-arms the
// refusal.

// Provenance classifies an existing config file against its digest stamp.
type Provenance int

const (
	// ProvenanceIntact: digest present and matching — the file is exactly as
	// setup wrote it.
	ProvenanceIntact Provenance = iota
	// ProvenanceLegacy: no digest line — written before digest stamping (or
	// by hand from scratch).
	ProvenanceLegacy
	// ProvenanceEdited: digest present but the content above it changed.
	ProvenanceEdited
)

// commentStyle renders the digest line in the host file's comment syntax.
type commentStyle struct {
	open  string
	close string
}

var (
	unitCommentStyle  = commentStyle{open: "# sesh-setup: ", close: ""}
	plistCommentStyle = commentStyle{open: "<!-- sesh-setup: ", close: " -->"}
)

// stamp appends the provenance digest line for body.
func (cs commentStyle) stamp(body []byte) []byte {
	sum := sha256.Sum256(body)
	line := cs.open + "sha256=" + hex.EncodeToString(sum[:]) + cs.close + "\n"
	return append(append([]byte{}, body...), []byte(line)...)
}

// split separates content into the body above the digest line and its
// provenance. For ProvenanceLegacy the body is the whole content.
func (cs commentStyle) split(content []byte) ([]byte, Provenance) {
	s := string(content)
	idx := strings.LastIndex(s, cs.open)
	if idx > 0 && s[idx-1] != '\n' {
		return content, ProvenanceLegacy
	}
	if idx < 0 {
		return content, ProvenanceLegacy
	}
	line := strings.TrimSuffix(s[idx:], "\n")
	if strings.Contains(line, "\n") {
		// Digest-shaped line that is not the last line: treat as content.
		return content, ProvenanceLegacy
	}
	digest, ok := strings.CutPrefix(line, cs.open)
	if !ok {
		return content, ProvenanceLegacy
	}
	if cs.close != "" {
		digest, ok = strings.CutSuffix(digest, cs.close)
		if !ok {
			return content, ProvenanceLegacy
		}
	}
	hexSum, ok := strings.CutPrefix(digest, "sha256=")
	if !ok || len(hexSum) != sha256.Size*2 {
		return content, ProvenanceLegacy
	}
	body := content[:idx]
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != hexSum {
		return body, ProvenanceEdited
	}
	return body, ProvenanceIntact
}
