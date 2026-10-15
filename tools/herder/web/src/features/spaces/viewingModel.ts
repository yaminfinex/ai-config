// Viewing marks an agent read once its panel has been on screen for the
// dwell. One second is long enough that ⌥←/→ tab cycling and a space
// switch passing through do not count, and short enough that glancing at a
// finished reply clears it without having to type.
export const viewDwellMs = 1000

export type ViewingState = Readonly<Record<string, number>>

type Group = { visible: boolean, focused: boolean, activeAgent?: string }

// viewedAgents is the agent seen now: the active panel of the focused group
// in the live (active space) dock, while it is visible and so is the
// document (owner, 2026-10-03: in a split only the focused panel counts as
// seen). The lens shows no panel, so the caller passes no groups there.
export function viewedAgents(groups: readonly Group[], documentVisible: boolean): string[] {
  if (!documentVisible) return []
  return groups.flatMap((group) => group.focused && group.visible && group.activeAgent ? [group.activeAgent] : []).slice(0, 1)
}

// nextViewing keeps each agent's start time while it stays viewed so a
// layout change elsewhere does not restart its dwell.
export function nextViewing(previous: ViewingState, viewed: readonly string[], now: number): ViewingState {
  const next: Record<string, number> = {}
  for (const name of viewed) next[name] = previous[name] ?? now
  const same = viewed.length === Object.keys(previous).length && viewed.every((name) => previous[name] !== undefined)
  return same ? previous : next
}

export function dwelledAgents(state: ViewingState, now: number, dwell = viewDwellMs): string[] {
  return Object.entries(state).filter(([, since]) => now - since >= dwell).map(([name]) => name)
}

// nextDwellDelay is how long until another viewed agent reaches the dwell,
// or null when every viewed agent already has.
export function nextDwellDelay(state: ViewingState, now: number, dwell = viewDwellMs): number | null {
  const waits = Object.values(state).map((since) => since + dwell - now).filter((wait) => wait > 0)
  return waits.length ? Math.min(...waits) : null
}
