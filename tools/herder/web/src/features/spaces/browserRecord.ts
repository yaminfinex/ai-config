// A small versioned localStorage record with a last-good backup, the shape
// the shell and layout stores already use: a corrupt primary falls back to
// the backup, and the backup is only rotated once a primary has parsed.

export type BrowserRecordState = { recovering: boolean, lastGoodRaw: string | null }

export function readBrowserRecord<T>(storage: Pick<Storage, 'getItem'>, key: string, parse: (raw: string | null) => T | null) {
  let primaryRaw: string | null = null
  let backupRaw: string | null = null
  try {
    primaryRaw = storage.getItem(key)
    backupRaw = storage.getItem(`${key}.last-good`)
  } catch { /* unavailable storage reads as empty */ }
  const primary = parse(primaryRaw)
  const backup = parse(backupRaw)
  return {
    value: primary ?? backup,
    state: { recovering: Boolean(!primary && backup), lastGoodRaw: primary ? primaryRaw : null } as BrowserRecordState,
  }
}

export function writeBrowserRecord(storage: Pick<Storage, 'setItem'>, key: string, raw: string, state: BrowserRecordState): BrowserRecordState {
  if (raw === state.lastGoodRaw) return state
  try {
    if (!state.recovering) storage.setItem(`${key}.last-good`, state.lastGoodRaw ?? raw)
    storage.setItem(key, raw)
    return { recovering: false, lastGoodRaw: raw }
  } catch {
    return state
  }
}

export function plainRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
