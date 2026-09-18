// The pane ids the fleet stream subscribes to for screen frames: proven
// screen panels, agent panels in screen mode, and agent panels showing the
// live tail. Each source is a record keyed by agent name that the panel sets
// while its view is mounted and clears when it unmounts, so a hidden panel or
// a listening agent contributes nothing.
export function screenSubscriptionPaneIDs(
  provenScreenPaneIDs: string[],
  agentNames: string[],
  agentScreenPanes: Record<string, string>,
  agentTailPanes: Record<string, string>,
) {
  const agentPanes = agentNames.flatMap((name) => [agentScreenPanes[name], agentTailPanes[name]].filter((paneID): paneID is string => Boolean(paneID)))
  return [...new Set([...provenScreenPaneIDs, ...agentPanes])]
}
