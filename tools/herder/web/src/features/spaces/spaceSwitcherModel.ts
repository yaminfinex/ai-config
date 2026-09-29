// The ⌥Tab space switcher, Windows Alt+Tab style: holding Option (Alt)
// and pressing Tab opens a most-recently-used list with the highlight on
// the previous space; Tab and Shift+Tab move it; releasing Option commits;
// Escape cancels. A quick tap commits before the list is ever shown, which
// flips to the last space. Losing window focus while holding CANCELS: the
// Option release then happens somewhere the owner is not looking, so a
// switch they cannot see would be a surprise; cancelling leaves them where
// they were.

export type SwitcherState =
  | { phase: 'idle' }
  | { phase: 'holding', order: readonly string[], index: number, shown: boolean }

export type SwitcherEvent =
  | { type: 'cycle', direction: 'forward' | 'backward', order: readonly string[] }
  | { type: 'step', direction: 'forward' | 'backward' }
  | { type: 'show' }
  | { type: 'release' }
  | { type: 'choose', id: string }
  | { type: 'cancel' }

export type SwitcherResult = { state: SwitcherState, commit?: string }

export const idleSwitcher: SwitcherState = { phase: 'idle' }
// The list waits this long before painting so a quick tap never flashes it.
export const switcherRevealDelayMs = 150

function wrap(index: number, length: number) {
  return (index + length) % length
}

export function reduceSwitcher(state: SwitcherState, event: SwitcherEvent): SwitcherResult {
  if (state.phase === 'idle') {
    if (event.type !== 'cycle' || event.order.length < 2) return { state }
    return { state: { phase: 'holding', order: event.order, index: event.direction === 'forward' ? 1 : event.order.length - 1, shown: false } }
  }
  switch (event.type) {
    case 'cycle':
    case 'step':
      return { state: { ...state, index: wrap(state.index + (event.direction === 'forward' ? 1 : -1), state.order.length) } }
    case 'show':
      return { state: state.shown ? state : { ...state, shown: true } }
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
