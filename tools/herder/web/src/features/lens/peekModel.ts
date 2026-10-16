import type { TranscriptEntry } from '../../types.ts'

// The peek is a read-only look at the selected card's visible agent, on the
// lens's right (owner #463614): a slim header, its recent transcript from
// the shared entries cache, no composer and no dock. Peeking is not
// viewing: it reads nothing, views no block and is never the focused panel.

// The peek's share of the lens width, this browser's (herder.web.lens.v1):
// half the lens by default (owner #475682).
export const peekWidth = 0.5
export const minPeekWidth = 0.2
export const maxPeekWidth = 0.6
const peekKeyStep = 0.02

// peekTail is how many of the transcript's newest entries the peek draws.
export const peekTail = 80

export function clampPeekWidth(width: number): number {
  return Math.min(maxPeekWidth, Math.max(minPeekWidth, Math.round(width * 1000) / 1000))
}

// resizedPeekWidth follows a drag of the peek's left edge: dragging left
// widens it.
export function resizedPeekWidth(start: number, lensWidth: number, dx: number): number {
  if (lensWidth <= 0) return clampPeekWidth(start)
  return clampPeekWidth(start - dx / lensWidth)
}

export function peekWidthFromKey(width: number, key: 'ArrowLeft' | 'ArrowRight'): number {
  return clampPeekWidth(width + (key === 'ArrowLeft' ? peekKeyStep : -peekKeyStep))
}

export function validPeekWidth(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= minPeekWidth && value <= maxPeekWidth
}

// peekWindow is the newest peekTail entries, with the unread divider (an
// index into all of them, -1 for none) moved into the window: a divider
// before the window puts every shown entry after it.
export function peekWindow(entries: readonly TranscriptEntry[], dividerAt: number): { entries: TranscriptEntry[], dividerAt: number } {
  const offset = Math.max(0, entries.length - peekTail)
  return { entries: entries.slice(offset), dividerAt: dividerAt < 0 ? -1 : Math.max(0, dividerAt - offset) }
}
