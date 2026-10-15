import { plainRecord, readBrowserRecord, writeBrowserRecord, type BrowserRecordState } from '../spaces/browserRecord.ts'
import { cardTextModes, type CardTextMode } from './cardTextModel.ts'
import { emptyLensPrefs, focusSizes, lensRows, type LensPrefs, type LensRow } from './lensModel.ts'
import { validPeekWidth } from './peekModel.ts'

// The lens rows, visible agents, card text and size, and the peek's width
// are this browser's (owner ruling, 2026-10-15), like the other web prefs: a
// versioned record with a last-good backup.
export const lensPrefsKey = 'herder.web.lens.v1'

function stringMap<T extends string>(value: unknown, valid: (item: unknown) => item is T): Record<string, T> | null {
  if (!plainRecord(value)) return null
  const result: Record<string, T> = {}
  for (const [key, item] of Object.entries(value)) if (valid(item)) result[key] = item
  return result
}

const isRow = (value: unknown): value is LensRow => lensRows.includes(value as LensRow)
const isName = (value: unknown): value is string => typeof value === 'string' && value.length > 0

// parseLensPrefs keeps every valid entry of a valid record, so one bad
// value written by a newer client does not cost the rest.
export function parseLensPrefs(raw: string | null): LensPrefs | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!plainRecord(value) || value.version !== 1) return null
    const rows = stringMap(value.rows, isRow)
    const visible = stringMap(value.visible, isName)
    if (!rows || !visible) return null
    const text = cardTextModes.includes(value.text as CardTextMode) ? value.text as CardTextMode : undefined
    const size = Number.isInteger(value.size) && (value.size as number) >= 0 && (value.size as number) < focusSizes.length ? value.size as number : undefined
    const peek = validPeekWidth(value.peek) ? value.peek : undefined
    return { rows, visible, ...(text ? { text } : {}), ...(size !== undefined ? { size } : {}), ...(peek !== undefined ? { peek } : {}) }
  } catch {
    return null
  }
}

export function readLensPrefs(storage: Pick<Storage, 'getItem'>) {
  const { value, state } = readBrowserRecord(storage, lensPrefsKey, parseLensPrefs)
  return { prefs: value ?? emptyLensPrefs, state }
}

export function writeLensPrefs(storage: Pick<Storage, 'setItem'>, prefs: LensPrefs, state: BrowserRecordState) {
  return writeBrowserRecord(storage, lensPrefsKey, JSON.stringify({ version: 1, rows: prefs.rows, visible: prefs.visible, text: prefs.text, size: prefs.size, peek: prefs.peek }), state)
}

// The viewed blocks: agents whose current block the owner has viewed. Local
// only, never in read.markers.
export const viewedBlocksKey = 'herder.web.viewed-blocks.v1'

export function parseViewedBlocks(raw: string | null): string[] | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!plainRecord(value) || value.version !== 1 || !Array.isArray(value.agents)) return null
    return [...new Set(value.agents.filter(isName))]
  } catch {
    return null
  }
}

export function readViewedBlocks(storage: Pick<Storage, 'getItem'>) {
  const { value, state } = readBrowserRecord(storage, viewedBlocksKey, parseViewedBlocks)
  return { blocks: new Set(value ?? []) as ReadonlySet<string>, state }
}

export function writeViewedBlocks(storage: Pick<Storage, 'setItem'>, blocks: ReadonlySet<string>, state: BrowserRecordState) {
  return writeBrowserRecord(storage, viewedBlocksKey, JSON.stringify({ version: 1, agents: [...blocks].sort() }), state)
}
