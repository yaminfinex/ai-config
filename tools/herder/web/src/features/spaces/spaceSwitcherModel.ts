// The ⌥Tab space switcher, Windows Alt+Tab style: holding Option (Alt)
// and pressing Tab opens a most-recently-used list with the highlight on
// the previous space; Tab and Shift+Tab move it; releasing Option commits;
// Escape cancels. The list paints on the first ⌥Tab with no reveal delay:
// a quick tap may flash it for a frame, which beats a laggy switcher, and
// still flips to the last space. Losing window focus while holding CANCELS: the
// Option release then happens somewhere the owner is not looking, so a
// switch they cannot see would be a surprise; cancelling leaves them where
// they were.

export type SwitcherState =
  | { phase: 'idle' }
  | { phase: 'holding', order: readonly string[], index: number }

export type SwitcherEvent =
  | { type: 'cycle', direction: 'forward' | 'backward', order: readonly string[] }
  | { type: 'step', direction: 'forward' | 'backward' }
  | { type: 'release' }
  | { type: 'choose', id: string }
  | { type: 'cancel' }

export type SwitcherResult = { state: SwitcherState, commit?: string }

export const idleSwitcher: SwitcherState = { phase: 'idle' }

function wrap(index: number, length: number) {
  return (index + length) % length
}

export function reduceSwitcher(state: SwitcherState, event: SwitcherEvent): SwitcherResult {
  if (state.phase === 'idle') {
    if (event.type !== 'cycle' || event.order.length < 2) return { state }
    return { state: { phase: 'holding', order: event.order, index: event.direction === 'forward' ? 1 : event.order.length - 1 } }
  }
  switch (event.type) {
    case 'cycle':
    case 'step':
      return { state: { ...state, index: wrap(state.index + (event.direction === 'forward' ? 1 : -1), state.order.length) } }
    case 'release': {
      const target = state.order[state.index]
      return { state: idleSwitcher, ...(target && state.index !== 0 ? { commit: target } : {}) }
    }
    case 'choose':
      return { state: idleSwitcher, ...(event.id !== state.order[0] && state.order.includes(event.id) ? { commit: event.id } : {}) }
    case 'cancel':
      return { state: idleSwitcher }
  }
}

export function highlightedSpace(state: SwitcherState): string | null {
  return state.phase === 'holding' ? state.order[state.index] ?? null : null
}
