import type { EntriesPage, EntryKind, TranscriptEntry } from '../../types.ts'
import { comparePositions, samePosition, type ReadMarker, type ReadPosition } from './readMarkerModel.ts'

export function entryPosition(session: string, entry: TranscriptEntry): ReadPosition {
  return { session, offset: entry.byteOffset, ts: entry.timestamp ?? '' }
}

// latestPosition is the newest entry the transcript window has rendered,
// or null while it has none.
export function latestPosition(page: EntriesPage | undefined): ReadPosition | null {
  const last = page?.entries?.at(-1)
  return page?.sessionId && last ? entryPosition(page.sessionId, last) : null
}

// positionBefore is the read position just before entries[index]: the
// entry above it, or one byte short of it when it opens the window.
export function positionBefore(session: string, entries: readonly TranscriptEntry[], index: number): ReadPosition | null {
  const entry = entries[index]
  if (!entry) return null
  const previous = entries[index - 1]
  return previous ? entryPosition(session, previous) : { session, offset: Math.max(0, entry.byteOffset - 1), ts: '' }
}

// A turn opens with what the owner, another agent or an outside channel sent it.
const turnOpeners = new Set<EntryKind>(['human_prompt', 'hcom_delivery_stub', 'hcom_delivery', 'task_notification', 'channel_message'])

// lastTurnStart is the index of the entry that opened the latest turn, the
// last entry when no opener is in the window, or -1 for an empty window.
export function lastTurnStart(entries: readonly TranscriptEntry[]): number {
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    if (!turnOpeners.has(entries[index]!.kind)) continue
    // A delivery stub and the delivery it announces open the turn together.
    return index > 0 && entries[index]!.kind === 'hcom_delivery' && entries[index - 1]!.kind === 'hcom_delivery_stub' ? index - 1 : index
  }
  return entries.length - 1
}

// markUnreadAt is a deliberate mark unread from entries[index]; with no
// loaded window it keeps the position and only marks unread.
export function markUnreadAt(marker: ReadMarker | undefined, page: EntriesPage | undefined, index: number): ReadMarker {
  const entries = page?.entries ?? []
  const pos = page?.sessionId ? positionBefore(page.sessionId, entries, index) : null
  return { turn: marker?.turn ?? 0, pos: pos ?? marker?.pos ?? null, at: marker?.at ?? 0, unread: true }
}

// markLastTurnUnread is the footer's, the menus' and ⌥U's mark unread:
// reading resumes at the start of the latest turn.
export function markLastTurnUnread(marker: ReadMarker | undefined, page: EntriesPage | undefined): ReadMarker {
  return markUnreadAt(marker, page, lastTurnStart(page?.entries ?? []))
}

// A position that only creeps forward inside a turn already read is
// written at most this often, so a streaming transcript is not a write per
// entry; a turn end, a cleared mark or a new session writes at once.
export const positionWriteMs = 5000

// readThrough is what reading for the dwell writes: the board's turn end
// and the newest rendered entry, never moving either backward, and clearing
// a mark unread. null when it would change nothing (or only creep the
// position sooner than positionWriteMs).
export function readThrough(marker: ReadMarker | undefined, turn: number | null, latest: ReadPosition | null, now: number): ReadMarker | null {
  const nextTurn = Math.max(marker?.turn ?? 0, turn ?? 0)
  const current = marker?.pos ?? null
  const pos = latest && comparePositions(current, marker?.at ?? 0, latest, now) > 0 ? latest : current
  if (marker && !marker.unread && nextTurn === marker.turn) {
    if (samePosition(pos, current)) return null
    if (current && pos && current.session === pos.session && now - marker.at < positionWriteMs) return null
  }
  return { turn: nextTurn, pos, at: now, unread: false }
}

// dividerIndex is where the quiet "new" divider goes: above the first
// entry after the position, in the same session only. -1 for none.
export function dividerIndex(entries: readonly TranscriptEntry[], session: string | undefined, pos: ReadPosition | null): number {
  if (!pos || !session || pos.session !== session) return -1
  return entries.findIndex((entry) => entry.byteOffset > pos.offset)
}

// viewedAtLabel is the footer's local 24-hour time (HH:MM) of the last read, or '' for an
// agent never read.
export function viewedAtLabel(at: number, now: number): string {
  if (!at) return ''
  const date = new Date(at)
  const time = date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
  return new Date(now).toDateString() === date.toDateString() ? time : `${date.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${time}`
}
