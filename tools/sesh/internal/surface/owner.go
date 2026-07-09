package surface

// View-time display-owner precedence (R15, spec §3.2) over the facts
// observation log:
//
//	SESSION_OWNER > tailnet identity (M4+) > OS user > hostname
//
// This is pure store/surface logic, revisable without touching any node —
// no precedence code may exist shipper-side (asserted by the module tests).
// The result is attribution, never authentication (I10): it affects display
// and grouping only, never access. Absence of SESSION_OWNER means nobody
// claimed the work tree and renders as absence; conflicting claims render
// as absence with a conflicting-claims label, not a pick.

// DisplayOwner is the view-time attribution for one session.
type DisplayOwner struct {
	// Name is the winning attribution value; empty is honest absence.
	Name string
	// Source names the winning fact's source, in attribution voice.
	Source string
	// Claimed is true when an identity fact won (SESSION_OWNER or tailnet
	// identity). Node-tier attributions (OS user, hostname) carry a Name
	// but group under node/OS-user on the page — same-named OS users on
	// different nodes are not one person (spec §4.4).
	Claimed bool
	// Conflict is true when distinct SESSION_OWNER observations exist for
	// this session; Name stays empty.
	Conflict bool
}

// DisplayOwner computes the precedence verdict for this session.
func (s SessionSummary) DisplayOwner() DisplayOwner {
	claims := distinctClaims(s.OwnerClaims)
	switch {
	case len(claims) > 1:
		return DisplayOwner{Conflict: true, Source: "conflicting SESSION_OWNER claims"}
	case len(claims) == 1:
		return DisplayOwner{Name: claims[0], Source: "SESSION_OWNER fact", Claimed: true}
	case s.TailnetIdentity != "":
		return DisplayOwner{Name: s.TailnetIdentity, Source: "tailnet identity", Claimed: true}
	case s.OSUser != "":
		return DisplayOwner{Name: s.OSUser, Source: "OS user (no owner claim)"}
	case s.Hostname != "":
		return DisplayOwner{Name: s.Hostname, Source: "hostname (no owner claim)"}
	default:
		return DisplayOwner{Source: "no attribution facts"}
	}
}

// distinctClaims dedups defensively while keeping first-observed order, so a
// repeated observation of the same owner can never masquerade as a conflict.
func distinctClaims(claims []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range claims {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}
