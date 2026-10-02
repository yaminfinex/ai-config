import type { ReadMarker, ReadPosition } from '../spaces/index.ts'

// The "new" divider holds the read position as it stood when the owner
// arrived at the agent, so it does not vanish while they read; it moves on
// the next arrival, or when the agent is marked unread (here or on another
// device) while it is open.
export type DividerSnapshot = Readonly<{ pos: ReadPosition | null, settled: boolean, active: boolean, unread: boolean }>

export const initialDividerSnapshot: DividerSnapshot = { pos: null, settled: false, active: false, unread: false }

function samePosition(a: ReadPosition | null, b: ReadPosition | null) {
  return a === b || (a !== null && b !== null && a.session === b.session && a.offset === b.offset)
}

// nextDividerSnapshot returns previous itself when nothing moved. An
// arrival before the agent's marker has loaded settles on the marker once
// it arrives. A deliberate unread holds its position (reading never
// advances it), so a new position while it stays unread is another mark,
// here or on another device, and moves the divider too.
export function nextDividerSnapshot(previous: DividerSnapshot, active: boolean, marker: ReadMarker | undefined): DividerSnapshot {
  const unread = Boolean(marker?.unread)
  const arriving = active && !previous.active
  const marked = unread && (!previous.unread || !samePosition(marker?.pos ?? null, previous.pos))
  const settled = arriving ? false : previous.settled
  const take = active && (arriving || marked || (!settled && marker !== undefined))
  const next = take ? { pos: marker?.pos ?? null, settled: marker !== undefined, active, unread } : { ...previous, settled, active, unread }
  return next.pos === previous.pos && next.settled === previous.settled && next.active === previous.active && next.unread === previous.unread ? previous : next
}

type MenuTarget = { closest: (selector: string) => { getAttribute: (name: string) => string | null } | null }

// blockMenuIndex is the entry index a right-click on the transcript marks
// unread from, or null to leave the browser's own menu: over selected
// text, a link or a text box, or outside any block.
export function blockMenuIndex(target: MenuTarget | null, selection: string): number | null {
  if (!target || selection.trim() || target.closest('a, input, textarea, select, [contenteditable="true"]')) return null
  const index = Number(target.closest('[data-entry-index]')?.getAttribute('data-entry-index') ?? NaN)
  return Number.isSafeInteger(index) && index >= 0 ? index : null
}
