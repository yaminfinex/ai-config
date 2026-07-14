package surface

import "regexp"

// Version-skew rendering policy (ops/README "Version-skew policy"): the
// support window is current + previous release, releases increment the patch
// component, so the window floor is the running store's own version with the
// patch decremented. Everything here degrades to "cannot judge" rather than
// erroring: a node the store cannot place is flagged unknown, and a store
// that cannot place itself (dev/untagged build) flags nobody — the nodes
// view never 500s over a version string.

// versionRE accepts the forms a shipper or release tag actually produces:
// "0.1.8", "v0.1.8", "sesh-v0.1.8", and git-describe derivatives like
// "sesh-v0.1.8-3-g1a2b3c4" or "sesh-v0.1.8-dirty" (which count as their base
// tag). Anything else — "dev", a bare commit hash — does not parse.
var versionRE = regexp.MustCompile(`^(?:sesh-)?v?(\d{1,9})\.(\d{1,9})\.(\d{1,9})(?:[.+-].*)?$`)

// parseVersion extracts the numeric release triple from a version token.
func parseVersion(s string) ([3]int, bool) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range 3 {
		n := 0
		for _, c := range m[i+1] {
			n = n*10 + int(c-'0')
		}
		v[i] = n
	}
	return v, true
}

// supportWindowFloor returns the oldest in-window release given the running
// store's version: one patch below current, or current itself when the patch
// is zero (the previous release's number is not derivable across a
// minor/major bump — conservative, and the skew policy says such a bump is a
// planned fleet event anyway). ok=false when current does not parse; no node
// may be flagged behind then.
func supportWindowFloor(current string) ([3]int, bool) {
	v, ok := parseVersion(current)
	if !ok {
		return [3]int{}, false
	}
	if v[2] > 0 {
		v[2]--
	}
	return v, true
}

// compareVersions orders release triples; nodes at or above the floor are in
// the window, and a node ahead of the store (mid-rollout) is never flagged.
func compareVersions(a, b [3]int) int {
	for i := range 3 {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
