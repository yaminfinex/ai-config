import { plainRecord, readBrowserRecord, writeBrowserRecord, type BrowserRecordState } from './browserRecord.ts'

// Read markers are browser-local: for each agent name, the newest turn-end
// id (the board's turn_end_id, a monotonic hcom event id) the owner has
// seen. v1 held context fingerprints and is not migrated: those agents are
// seeded afresh.
export const readMarkersKey = 'herder.web.read-markers.v2'

export type ReadMarkers = Readonly<Record<string, number>>

export function parseReadMarkers(raw: string | null): ReadMarkers | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!plainRecord(value) || value.version !== 2 || !plainRecord(value.markers)) return null
    const entries = Object.entries(value.markers)
    if (!entries.every(([, marker]) => Number.isSafeInteger(marker) && (marker as number) > 0)) return null
    return Object.fromEntries(entries) as ReadMarkers
  } catch {
    return null
  }
}

export function readReadMarkers(storage: Pick<Storage, 'getItem'>) {
  const { value, state } = readBrowserRecord(storage, readMarkersKey, parseReadMarkers)
  return { markers: value ?? {}, state }
}

export function writeReadMarkers(storage: Pick<Storage, 'setItem'>, markers: ReadMarkers, state: BrowserRecordState) {
  return writeBrowserRecord(storage, readMarkersKey, JSON.stringify({ version: 2, markers }), state)
}

// setMarkers applies updates, returning the same object when nothing changed.
export function setMarkers(markers: ReadMarkers, updates: Record<string, number>): ReadMarkers {
  const changed = Object.entries(updates).filter(([name, marker]) => markers[name] !== marker)
  if (changed.length === 0) return markers
  return { ...markers, ...Object.fromEntries(changed) }
}

// keepMarkers drops the markers of agents in neither keep set, returning the
// same object when every marker stays. There is no count cap: the kept set
// is bounded by the open panels and the live fleet.
export function keepMarkers(markers: ReadMarkers, keep: ReadonlySet<string>): ReadMarkers {
  const names = Object.keys(markers)
  if (names.every((name) => keep.has(name))) return markers
  return Object.fromEntries(names.filter((name) => keep.has(name)).map((name) => [name, markers[name]]))
}
