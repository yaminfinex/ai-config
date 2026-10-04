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

// Scroll a selection into view once. Only "not rendered yet" retries (up to the cap);
// once the line has been centered or scrolled to, later renders leave the user's scroll alone.
export function planLineScroll(previous: LineScrollState | undefined, selection: LineSelection, observation: LineObservation) {
  const same = previous?.path === selection.path && previous.content === selection.content && previous.line === selection.line
  const state = same ? previous : { ...selection, attempts: 0, done: false }
  if (state.done || state.attempts >= MAX_LINE_SCROLL_ATTEMPTS) return { scroll: false, next: state }
  if (observation === 'missing') return { scroll: false, next: { ...state, attempts: state.attempts + 1 } }
  return { scroll: observation === 'off-centre', next: { ...state, done: true } }
}
