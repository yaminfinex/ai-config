export const LINE_CENTER_TOLERANCE_PX = 24
export const MAX_LINE_SCROLL_ATTEMPTS = 8

type VerticalRect = Pick<DOMRect, 'top' | 'bottom'>

export function isLineCentered(line: VerticalRect, container: VerticalRect) {
  const lineCenter = (line.top + line.bottom) / 2
  const containerCenter = (container.top + container.bottom) / 2
  return Math.abs(lineCenter - containerCenter) <= LINE_CENTER_TOLERANCE_PX
}

export type LineSelection = { path: string, content: string, line: number }
export type LineScrollState = LineSelection & { attempts: number, done: boolean }
export type LineObservation = 'missing' | 'centered' | 'off-centre'

// Settle a selection on its line: re-centre after each render (Shiki highlighting moves deep
// lines after the first scroll) until it is centered, the user scrolls, or the attempts run out.
export function planLineScroll(previous: LineScrollState | undefined, selection: LineSelection, observation: LineObservation) {
  const same = previous?.path === selection.path && previous.content === selection.content && previous.line === selection.line
  const state = same ? previous : { ...selection, attempts: 0, done: false }
  if (state.done || state.attempts >= MAX_LINE_SCROLL_ATTEMPTS) return { scroll: false, next: state }
  if (observation === 'centered') return { scroll: false, next: { ...state, done: true } }
  return { scroll: observation === 'off-centre', next: { ...state, attempts: state.attempts + 1 } }
}

export const USER_SCROLL_EVENTS = ['wheel', 'touchstart', 'pointerdown', 'keydown'] as const

// The user took over scrolling, so the selection they are on is settled.
export function userScrolled(state: LineScrollState | undefined) {
  return state && { ...state, done: true }
}
