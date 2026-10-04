import { compareStateVersions } from '../../shared/stateVersion.ts'
import type { GenericStateRow } from '../../shared/stateSync.ts'
import type { ReadMarkersV2 } from './readMarkerStore.ts'

// A read position is the last transcript entry the owner has read: its
// session, its byte offset in that session's transcript, and its timestamp
// ('' when the entry had none).
export type ReadPosition = Readonly<{ session: string, offset: number, ts: string }>

// A read marker is what the owner has read of one agent, keyed by agent
// name. turn is the newest turn-end id (the board's turn_end_id) read, 0
// while the baseline is unknown; pos is where in the transcript reading
// reached, null until a transcript has been read; at is when (ms, 0 for
// never); unread is a deliberate "mark unread".
export type ReadMarker = Readonly<{ turn: number, pos: ReadPosition | null, at: number, unread: boolean }>
export type ReadMarkers = Readonly<Record<string, ReadMarker>>

export type ReadMarkerRow = ReadMarker & { key: string, updated: number, writeID: string, deleted: boolean }

// A weak write (silent seeding, the v2 migration) carries the lowest
// version, so any real write another device made wins last-write-wins.
export const weakUpdated = 1

export const baselineMarker = (turn: number): ReadMarker => ({ turn, pos: null, at: 0, unread: false })

function parsePosition(value: unknown): ReadPosition | null | undefined {
  if (value === null) return null
  if (!value || typeof value !== 'object') return undefined
  const pos = value as Partial<ReadPosition>
  if (typeof pos.session !== 'string' || !pos.session || !Number.isSafeInteger(pos.offset) || (pos.offset as number) < 0 || typeof pos.ts !== 'string') return undefined
  return { session: pos.session, offset: pos.offset as number, ts: pos.ts }
}

export function parseMarker(value: unknown): ReadMarker | null {
  if (!value || typeof value !== 'object') return null
  const marker = value as Partial<Record<keyof ReadMarker, unknown>>
  const pos = parsePosition(marker.pos)
  if (!Number.isSafeInteger(marker.turn) || (marker.turn as number) < 0 || pos === undefined ||
    !Number.isSafeInteger(marker.at) || (marker.at as number) < 0 || typeof marker.unread !== 'boolean') return null
  return { turn: marker.turn as number, pos, at: marker.at as number, unread: marker.unread }
}

export function parseMarkerRow(value: unknown): ReadMarkerRow | null {
  if (!value || typeof value !== 'object') return null
  const row = value as Partial<GenericStateRow>
  if (typeof row.key !== 'string' || !row.key || typeof row.writeID !== 'string' || !row.writeID ||
    typeof row.updated !== 'number' || !Number.isFinite(row.updated) || typeof row.deleted !== 'boolean') return null
  if (row.deleted) return { key: row.key, ...baselineMarker(0), updated: row.updated, writeID: row.writeID, deleted: true }
  const marker = parseMarker(row.value)
  return marker ? { key: row.key, ...marker, updated: row.updated, writeID: row.writeID, deleted: false } : null
}

export function markerOf(row: ReadMarker): ReadMarker {
  return { turn: row.turn, pos: row.pos, at: row.at, unread: row.unread }
}

export function markerStateRow(row: ReadMarkerRow): GenericStateRow {
  return {
    key: row.key,
    value: row.deleted ? null : { ...markerOf(row), updated: row.updated },
    updated: row.updated,
    writeID: row.writeID,
    deleted: row.deleted,
  }
}

// comparePositions orders two positions: within one session by offset;
// across sessions the one read more recently is ahead. Positive when
// right is further than left.
export function comparePositions(left: ReadPosition | null, leftAt: number, right: ReadPosition | null, rightAt: number): number {
  if (!left || !right) return (right ? 1 : 0) - (left ? 1 : 0)
  if (left.session === right.session) return right.offset - left.offset
  return rightAt - leftAt
}

// mergeMarkerRow resolves an incoming row against the one held. The newer
// version wins, but reading never moves backward: an older device's stale
// read must not relight a turn or rewind a position another device already
// read past. The one exception is a newer deliberate mark unread, which
// stands exactly as written. repair is true when the merged marker is
// ahead of the winning row, so the holder republishes it and the server
// converges. A weak row only fills an empty slot: against a real row it
// takes no part in the merge, so a seed never clears a turn the real
// marker keeps unread.
export function mergeMarkerRow(current: ReadMarkerRow | undefined, incoming: ReadMarkerRow): { row: ReadMarkerRow, repair: boolean } {
  if (!current) return { row: incoming, repair: false }
  const currentWeak = current.updated === weakUpdated
  if (currentWeak !== (incoming.updated === weakUpdated)) return { row: currentWeak ? incoming : current, repair: false }
  const incomingWins = compareStateVersions(incoming.updated, incoming.writeID, current.updated, current.writeID) > 0
  const winner = incomingWins ? incoming : current
  const loser = incomingWins ? current : incoming
  if (winner.deleted || winner.unread || loser.deleted) return { row: winner, repair: false }
  const loserAhead = comparePositions(winner.pos, winner.at, loser.pos, loser.at) > 0
  const merged: ReadMarkerRow = {
    ...winner,
    turn: Math.max(winner.turn, loser.turn),
    pos: loserAhead ? loser.pos : winner.pos,
    at: Math.max(winner.at, loser.at),
  }
  return sameMarker(winner, merged) ? { row: winner, repair: false } : { row: merged, repair: true }
}

// migratedMarkerRows seeds read.markers from the browser-local v2 markers,
// weakly, so a row another device already wrote is never clobbered.
export function migratedMarkerRows(legacy: ReadMarkersV2, writeID: string): ReadMarkerRow[] {
  return Object.entries(legacy).map(([key, turn]) => ({ key, ...baselineMarker(turn), updated: weakUpdated, writeID, deleted: false }))
}

export function samePosition(left: ReadPosition | null, right: ReadPosition | null) {
  return left === right || (!!left && !!right && left.session === right.session && left.offset === right.offset && left.ts === right.ts)
}

export function sameMarker(left: ReadMarker | undefined, right: ReadMarker) {
  return !!left && left.turn === right.turn && samePosition(left.pos, right.pos) && left.at === right.at && left.unread === right.unread
}

// nextArmed tracks which manual unreads the dwell may clear. A mark is held
// while its agent stays viewed and arms once the agent has been seen not
// viewed, so leaving and coming back clears it and staying on it does not.
// Arming waits for a ready dock so a reload restoring the viewed panel does
// not count as leaving it.
export function nextArmed(armed: ReadonlySet<string>, markers: ReadMarkers, viewed: readonly string[], ready: boolean): ReadonlySet<string> {
  const next = new Set<string>()
  for (const [name, marker] of Object.entries(markers)) {
    if (!marker.unread) continue
    if (armed.has(name) || (ready && !viewed.includes(name))) next.add(name)
  }
  return next.size === armed.size && [...next].every((name) => armed.has(name)) ? armed : next
}
