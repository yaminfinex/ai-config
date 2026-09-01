import { defaultRailPreferences, type RailPreferences } from './utilityRailModel.ts'

export const shellStorageKey = 'herder.web.shell.v1'
export const shellStorageBackupKey = 'herder.web.shell.v1.last-good'

export type StoredShellPreferences = {
  version: 1
  rails: RailPreferences
  expandedItems?: string[]
  knownWorkspaceItems?: string[]
}

type UnknownRecord = Record<string, unknown>

function record(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function strings(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

function rail(value: unknown) {
  if (!record(value) || typeof value.width !== 'number' || !Number.isFinite(value.width) || typeof value.collapsed !== 'boolean') return null
  return { width: Math.max(200, Math.min(440, value.width)), collapsed: value.collapsed }
}

export function parseShellPreferences(raw: string | null): StoredShellPreferences | null {
  try {
    const value: unknown = JSON.parse(raw ?? '')
    if (!record(value) || value.version !== 1 || !record(value.rails) ||
      (value.expandedItems !== undefined && !strings(value.expandedItems)) ||
      (value.knownWorkspaceItems !== undefined && !strings(value.knownWorkspaceItems))) return null
    const fleet = rail(value.rails.fleet)
    const notes = rail(value.rails.notes)
    if (!fleet || !notes) return null
    return {
      version: 1,
      rails: { fleet, notes },
      ...(value.expandedItems === undefined ? {} : { expandedItems: value.expandedItems }),
      ...(value.knownWorkspaceItems === undefined ? {} : { knownWorkspaceItems: value.knownWorkspaceItems }),
    }
  } catch {
    return null
  }
}

export function readShellPreferences(storage: Pick<Storage, 'getItem'>) {
  const primaryRaw = storage.getItem(shellStorageKey)
  const backupRaw = storage.getItem(shellStorageBackupKey)
  const primary = parseShellPreferences(primaryRaw)
  const backup = parseShellPreferences(backupRaw)
  return {
    stored: primary ?? backup ?? { version: 1 as const, rails: defaultRailPreferences() },
    recovering: Boolean(!primary && backup),
    lastGoodRaw: primary ? primaryRaw : null,
  }
}

export function writeShellPreferences(
  storage: Pick<Storage, 'setItem'>,
  raw: string,
  state: { recovering: boolean, lastGoodRaw: string | null },
) {
  try {
    if (!state.recovering) storage.setItem(shellStorageBackupKey, state.lastGoodRaw ?? raw)
    storage.setItem(shellStorageKey, raw)
    return { recovering: false, lastGoodRaw: raw }
  } catch {
    return state
  }
}
