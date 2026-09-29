import { plainRecord, readBrowserRecord, writeBrowserRecord, type BrowserRecordState } from './browserRecord.ts'

// Read markers are browser-local: for each agent name, the turn-end
// fingerprint (see spaceAttentionModel.turnFingerprint) the owner has seen.
// An empty string records "seen before any turn ended here".
export const readMarkersKey = 'herder.web.read-markers.v1'
export const maxReadMarkers = 400

export type ReadMarkers = Readonly<Record<string, string>>

export function parseReadMarkers(raw: string | null): ReadMarkers | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!plainRecord(value) || value.version !== 1 || !plainRecord(value.markers)) return null
    const entries = Object.entries(value.markers)
    if (!entries.every(([, marker]) => typeof marker === 'string')) return null
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
  return writeBrowserRecord(storage, readMarkersKey, JSON.stringify({ version: 1, markers }), state)
}

// setMarkers applies updates most-recent-last so the cap drops the agents
// touched longest ago; it returns the same object when nothing changed.
export function setMarkers(markers: ReadMarkers, updates: Record<string, string>): ReadMarkers {
  const changed = Object.entries(updates).filter(([name, marker]) => markers[name] !== marker)
  if (changed.length === 0) return markers
  const next: Record<string, string> = { ...markers }
  for (const [name, marker] of changed) {
    delete next[name]
    next[name] = marker
  }
  const names = Object.keys(next)
  for (const name of names.slice(0, Math.max(0, names.length - maxReadMarkers))) delete next[name]
  return next
}
