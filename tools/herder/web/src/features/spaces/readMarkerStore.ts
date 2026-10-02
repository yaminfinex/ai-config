import { defaultRandomID } from '../notes/notesStore.ts'
import type { GenericStateRow } from '../../shared/stateSync.ts'
import { plainRecord, readBrowserRecord } from './browserRecord.ts'
import {
  markerOf,
  markerStateRow,
  mergeMarkerRow,
  migratedMarkerRows,
  parseMarkerRow,
  sameMarker,
  weakUpdated,
  type ReadMarker,
  type ReadMarkerRow,
  type ReadMarkers,
} from './readMarkerModel.ts'

// v2 read markers were browser-local: for each agent name, the newest
// turn-end id seen. They are only read now, once, to seed read.markers.
export const readMarkersKey = 'herder.web.read-markers.v2'
export const readMarkerRowsKey = 'herder.web.read-markers.v3:rows'

export type ReadMarkersV2 = Readonly<Record<string, number>>

export function parseReadMarkers(raw: string | null): ReadMarkersV2 | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!plainRecord(value) || value.version !== 2 || !plainRecord(value.markers)) return null
    const entries = Object.entries(value.markers)
    if (!entries.every(([, marker]) => Number.isSafeInteger(marker) && (marker as number) > 0)) return null
    return Object.fromEntries(entries) as ReadMarkersV2
  } catch {
    return null
  }
}

export function readReadMarkers(storage: Pick<Storage, 'getItem'>) {
  const { value, state } = readBrowserRecord(storage, readMarkersKey, parseReadMarkers)
  return { markers: value ?? {}, state }
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>

type Options = {
  storage?: StorageLike | null
  now?: () => number
  randomID?: () => string
}

export type ReadMarkerStore = {
  markers: () => ReadMarkers
  rows: () => GenericStateRow[]
  merge: (rows: GenericStateRow[]) => void
  // apply writes each changed marker; weak writes only create rows.
  apply: (updates: Record<string, ReadMarker>, options?: { weak?: boolean }) => void
  // prune forgets rows locally. It never writes a delete: an agent absent
  // here may still be open on another device.
  prune: (keep: ReadonlySet<string>) => void
  subscribe: (listener: () => void) => () => void
  subscribeMutations: (listener: (rows: GenericStateRow[]) => void) => () => void
}

export function parseMarkerRows(raw: string | null): ReadMarkerRow[] | null {
  try {
    const value: unknown = JSON.parse(raw ?? 'null')
    return Array.isArray(value) ? value.flatMap((row) => parseMarkerRow(row) ?? []) : null
  } catch {
    return null
  }
}

// createReadMarkerStore holds the read.markers rows this browser knows. They
// persist in localStorage, so the marks work offline and before the first
// pull; the first run without them seeds from the v2 markers.
export function createReadMarkerStore(options: Options = {}): ReadMarkerStore {
  const storage = options.storage === undefined ? browserStorage() : options.storage
  const now = options.now ?? Date.now
  const randomID = options.randomID ?? defaultRandomID
  const rows = new Map<string, ReadMarkerRow>()
  const listeners = new Set<() => void>()
  const mutationListeners = new Set<(rows: GenericStateRow[]) => void>()
  let snapshot: ReadMarkers = {}

  let stored: ReadMarkerRow[] | null = null
  try { stored = parseMarkerRows(storage?.getItem(readMarkerRowsKey) ?? null) } catch { /* starts empty */ }
  const initial = stored ?? (storage ? migratedMarkerRows(readReadMarkers(storage).markers, randomID()) : [])
  for (const row of initial) rows.set(row.key, mergeMarkerRow(rows.get(row.key), row).row)

  const persist = () => {
    try { storage?.setItem(readMarkerRowsKey, JSON.stringify([...rows.values()].map(markerStateRow))) } catch { /* sync still carries the rows */ }
  }
  const changed = () => {
    snapshot = Object.fromEntries([...rows.values()].flatMap((row) => row.deleted ? [] : [[row.key, markerOf(row)]]))
    persist()
    for (const listener of listeners) listener()
  }
  const publish = (written: ReadMarkerRow[]) => {
    if (written.length === 0) return
    const stateRows = written.map(markerStateRow)
    for (const listener of mutationListeners) listener(stateRows)
  }
  const version = (current: ReadMarkerRow | undefined, weak: boolean) => !current && weak ? weakUpdated : Math.max(now(), (current?.updated ?? 0) + 1)
  if (stored === null) persist()
  snapshot = Object.fromEntries([...rows.values()].flatMap((row) => row.deleted ? [] : [[row.key, markerOf(row)]]))

  return {
    markers: () => snapshot,
    rows: () => [...rows.values()].map(markerStateRow),
    merge(incoming) {
      let dirty = false
      const repairs: ReadMarkerRow[] = []
      for (const raw of incoming) {
        const row = parseMarkerRow(raw)
        if (!row) continue
        const current = rows.get(row.key)
        const merged = mergeMarkerRow(current, row)
        if (merged.repair) {
          const repaired = { ...merged.row, updated: version(merged.row, false), writeID: randomID() }
          rows.set(row.key, repaired)
          repairs.push(repaired)
          dirty = true
        } else if (merged.row !== current) {
          rows.set(row.key, merged.row)
          dirty = true
        }
      }
      if (!dirty) return
      changed()
      // Republish after the pull that delivered the rows has settled.
      if (repairs.length) queueMicrotask(() => publish(repairs))
    },
    apply(updates, applyOptions = {}) {
      const written: ReadMarkerRow[] = []
      for (const [key, marker] of Object.entries(updates)) {
        const current = rows.get(key)
        const live = current && !current.deleted ? current : undefined
        if (applyOptions.weak && live) continue
        if (sameMarker(live, marker)) continue
        const row = { key, ...markerOf(marker), updated: version(current, Boolean(applyOptions.weak)), writeID: randomID(), deleted: false }
        rows.set(key, row)
        written.push(row)
      }
      if (written.length === 0) return
      changed()
      publish(written)
    },
    prune(keep) {
      const gone = [...rows.keys()].filter((key) => !keep.has(key))
      if (gone.length === 0) return
      for (const key of gone) rows.delete(key)
      changed()
    },
    subscribe(listener) {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
    subscribeMutations(listener) {
      mutationListeners.add(listener)
      return () => { mutationListeners.delete(listener) }
    },
  }
}

function browserStorage(): StorageLike | null {
  try { return window.localStorage } catch { return null }
}
