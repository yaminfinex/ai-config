// The pane ids the fleet stream subscribes to for screen frames: agent panels
// showing the live tail. The source is a record keyed by agent name that the
// panel sets while its tail is mounted and clears when it unmounts, so a
// hidden panel or a listening agent contributes nothing.
export function screenSubscriptionPaneIDs(agentNames: string[], agentTailPanes: Record<string, string>) {
  return [...new Set(agentNames.flatMap((name) => agentTailPanes[name] ? [agentTailPanes[name]] : []))]
}
