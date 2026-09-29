import { plainRecord, readBrowserRecord, writeBrowserRecord, type BrowserRecordState } from './browserRecord.ts'

// Most-recently-used space order for the ⌥Tab switcher. Browser-local and
// touched on every switch, whatever started it.
export const spaceMRUKey = 'herder.web.spaces.mru.v1'
export const maxSpaceMRU = 64

export function parseSpaceMRU(raw: string | null): string[] | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!plainRecord(value) || value.version !== 1 || !Array.isArray(value.order) ||
      !value.order.every((id) => typeof id === 'string')) return null
    return [...new Set(value.order as string[])]
  } catch {
    return null
  }
}

export function readSpaceMRU(storage: Pick<Storage, 'getItem'>) {
  const { value, state } = readBrowserRecord(storage, spaceMRUKey, parseSpaceMRU)
  return { order: value ?? [], state }
}

export function writeSpaceMRU(storage: Pick<Storage, 'setItem'>, order: readonly string[], state: BrowserRecordState) {
  return writeBrowserRecord(storage, spaceMRUKey, JSON.stringify({ version: 1, order }), state)
}

export function touchSpaceMRU(order: readonly string[], id: string): readonly string[] {
  if (order[0] === id) return order
  return [id, ...order.filter((item) => item !== id)].slice(0, maxSpaceMRU)
}

// mruSpaceIDs orders the live spaces most recent first; spaces never
// visited in this browser follow in their list order.
export function mruSpaceIDs(order: readonly string[], spaces: readonly { id: string }[], activeID: string | null): string[] {
  const live = spaces.map((space) => space.id)
  const known = new Set(live)
  const touched = activeID && known.has(activeID) ? touchSpaceMRU(order, activeID) : order
  const recent = touched.filter((id) => known.has(id))
  const seen = new Set(recent)
  return [...recent, ...live.filter((id) => !seen.has(id))]
}
