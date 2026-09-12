// folderTreeModel holds the pure width/visibility rules for the folder
// panel's tree column: a draggable, keyboard-resizable width bounded to the
// panel, persisted per browser under its own versioned key.

export const folderTreeStorageKey = 'herder.web.folder-tree.v1'
export const folderTreeMinWidth = 150
export const folderTreeMaxFraction = 0.6
export const folderTreeDefaultWidth = 220
export const folderTreeKeyStep = 10

export type FolderTreePreferences = { version: 1, width: number, hidden: boolean }

export function folderTreeMaxWidth(panelWidth: number) {
  if (!Number.isFinite(panelWidth) || panelWidth <= 0) return Number.POSITIVE_INFINITY
  return Math.max(folderTreeMinWidth, Math.floor(panelWidth * folderTreeMaxFraction))
}

export function clampFolderTreeWidth(width: number, panelWidth: number) {
  if (!Number.isFinite(width)) return folderTreeDefaultWidth
  return Math.round(Math.min(folderTreeMaxWidth(panelWidth), Math.max(folderTreeMinWidth, width)))
}

export function resizedFolderTreeWidth(width: number, pointerDelta: number, panelWidth: number) {
  return clampFolderTreeWidth(width + pointerDelta, panelWidth)
}

export function folderTreeWidthFromKey(width: number, key: string, panelWidth: number) {
  if (key === 'ArrowLeft') return clampFolderTreeWidth(width - folderTreeKeyStep, panelWidth)
  if (key === 'ArrowRight') return clampFolderTreeWidth(width + folderTreeKeyStep, panelWidth)
  if (key === 'Home') return clampFolderTreeWidth(folderTreeMinWidth, panelWidth)
  if (key === 'End') return clampFolderTreeWidth(folderTreeMaxWidth(panelWidth), panelWidth)
  return null
}

export function parseFolderTreePreferences(raw: string | null): FolderTreePreferences | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (typeof value !== 'object' || value === null || Array.isArray(value)) return null
    const record = value as Record<string, unknown>
    if (record.version !== 1 || typeof record.width !== 'number' || !Number.isFinite(record.width)) return null
    if (record.hidden !== undefined && typeof record.hidden !== 'boolean') return null
    return { version: 1, width: Math.max(folderTreeMinWidth, Math.round(record.width)), hidden: record.hidden === true }
  } catch {
    return null
  }
}

export function folderTreePreferencesValue(width: number, hidden: boolean): FolderTreePreferences {
  return { version: 1, width: Math.max(folderTreeMinWidth, Math.round(width)), hidden }
}

export function readFolderTreePreferences(storage: Pick<Storage, 'getItem'> | null): FolderTreePreferences {
  try {
    return parseFolderTreePreferences(storage?.getItem(folderTreeStorageKey) ?? null) ?? folderTreePreferencesValue(folderTreeDefaultWidth, false)
  } catch {
    return folderTreePreferencesValue(folderTreeDefaultWidth, false)
  }
}

export function writeFolderTreePreferences(storage: Pick<Storage, 'setItem'> | null, value: FolderTreePreferences) {
  try { storage?.setItem(folderTreeStorageKey, JSON.stringify(value)) } catch { /* storage full or blocked: the store keeps the value session-only */ }
}

export type FolderTreePreferencesPatch = { width?: number, hidden?: boolean }

export type FolderTreePreferencesStore = {
  get: () => FolderTreePreferences
  set: (patch: FolderTreePreferencesPatch) => FolderTreePreferences
  subscribe: (listener: () => void) => () => void
}

// createFolderTreePreferencesStore is one in-memory value per document: it
// is the truth for every mounted folder panel, storage is only a best-effort
// mirror read once (lazily) and written on every set. A blocked write or a
// stale stored value therefore never changes what the session shows, and
// two panels see each other's changes at once.
export function createFolderTreePreferencesStore(storage: () => (Pick<Storage, 'getItem' | 'setItem'> | null)): FolderTreePreferencesStore {
  let current: FolderTreePreferences | null = null
  const listeners = new Set<() => void>()
  const get = () => (current ??= readFolderTreePreferences(safeStorage(storage)))
  return {
    get,
    set: (patch) => {
      const base = get()
      const value = folderTreePreferencesValue(patch.width ?? base.width, patch.hidden ?? base.hidden)
      current = value
      writeFolderTreePreferences(safeStorage(storage), value)
      listeners.forEach((listener) => listener())
      return value
    },
    subscribe: (listener) => {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
  }
}

function safeStorage(storage: () => (Pick<Storage, 'getItem' | 'setItem'> | null)) {
  try { return storage() } catch { return null }
}

export const folderTreePreferences = createFolderTreePreferencesStore(() => (typeof window === 'undefined' ? null : window.localStorage))
