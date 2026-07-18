// Package agentfamily owns predicates shared across lifecycle packages.
package agentfamily

// HcomCapable is the single source of truth for agents routed through hcom.
func HcomCapable(agent string) bool {
	switch agent {
	case "claude", "codex", "gemini", "grok", "pi":
		return true
	default:
		return false
	}
}

// CompletionOwner names the family-specific process that converges a live
// child after its foreground launcher times out before canonical seating.
func CompletionOwner(agent string) string {
	if agent == "grok" {
		return "bridge supervisor"
	}
	return "sidecar"
}
