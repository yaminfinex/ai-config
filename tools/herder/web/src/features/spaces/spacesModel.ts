import {
  legacyLayoutStorageKey,
  layoutStorageBackupKey,
  layoutStorageKey,
  parseLegacyLayout,
  parseStoredLayout,
  readStoredLayout,
  spaceLayoutBackupPrefix,
  spaceLayoutPrefix,
  spaceLayoutRecoveryPrefix,
  v2LayoutStorageKey,
  type LegacyLayout,
  type StoredLayout,
} from '../layout/dockLayout.ts'
import { defaultRailPreferences } from '../layout/utilityRailModel.ts'
import { shellStorageBackupKey, shellStorageKey } from '../layout/shellPreferences.ts'

export const mainSpaceID = 'main'
export const spacesRecordPrefix = 'herder.web.spaces.v1:'
export const spacesBackupPrefix = 'herder.web.spaces.v1.last-good:'
export const spacesRecoveryPrefix = 'herder.web.spaces.v1.recovery:'
export const activeSpaceSessionKey = 'herder.web.spaces.active.v1'
export const lastActiveSpaceKey = 'herder.web.spaces.last-active.v1'
export const migrationMarkerKey = 'herder.web.spaces.migration.v1'

export type SpaceDefinition = {
  id: string
  name: string
  order: number
  created: number
  updated: number
  deleted?: true
}

export type StoredSpaceRecord = { version: 1, writeID: string, record: SpaceDefinition }

export type LegacyLayoutFamilies = {
  stored: StoredLayout | null
  backup: StoredLayout | null
  legacy: LegacyLayout | null
  recovering: boolean
  lastGoodRaw: string | null
}

export type SpacesInitialization =
  | { mode: 'spaces', activeSpaceID: typeof mainSpaceID }
  | { mode: 'legacy', activeSpaceID: null, legacy: LegacyLayoutFamilies, problem: string }

type MigrationStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export function spaceRecordKey(id: string) { return `${spacesRecordPrefix}${encodeURIComponent(id)}` }
export function spaceBackupKey(id: string) { return `${spacesBackupPrefix}${encodeURIComponent(id)}` }
export function spaceRecoveryKey(id: string) { return `${spacesRecoveryPrefix}${encodeURIComponent(id)}` }
export function spaceLayoutKey(id: string) { return `${spaceLayoutPrefix}${encodeURIComponent(id)}` }
export function spaceLayoutBackupKey(id: string) { return `${spaceLayoutBackupPrefix}${encodeURIComponent(id)}` }
export function layoutRecoveryKey(id: string) { return `${spaceLayoutRecoveryPrefix}${encodeURIComponent(id)}` }
export { shellStorageKey }

export function readLegacyLayoutFamilies(storage: Pick<Storage, 'getItem'>): LegacyLayoutFamilies {
  const layouts = readStoredLayout(storage)
  let stored = layouts.stored
  let legacy: LegacyLayout | null = null
  if (!stored) stored = parseStoredLayout(storage.getItem(v2LayoutStorageKey))
  if (!stored) legacy = parseLegacyLayout(storage.getItem(legacyLayoutStorageKey))
  return {
    stored,
    backup: layouts.backup,
    legacy,
    recovering: layouts.recovering,
    lastGoodRaw: layouts.lastGoodRaw,
  }
}

function markerValid(raw: string | null) {
  try {
    const value = JSON.parse(raw ?? '') as { version?: unknown, mainSpaceID?: unknown }
    return value.version === 1 && value.mainSpaceID === mainSpaceID
  } catch {
    return false
  }
}

function verifiedWrite(storage: Pick<Storage, 'getItem' | 'setItem'>, key: string, raw: string) {
  storage.setItem(key, raw)
  if (storage.getItem(key) !== raw) throw new Error(`storage did not retain ${key}`)
}

export function initializeSpaces(storage: MigrationStorage): SpacesInitialization {
  try {
    const legacy = readLegacyLayoutFamilies(storage)
    if (markerValid(storage.getItem(migrationMarkerKey)) && storage.getItem(spaceRecordKey(mainSpaceID)) &&
      storage.getItem(shellStorageKey) && storage.getItem(spaceLayoutKey(mainSpaceID))) {
      return { mode: 'spaces', activeSpaceID: mainSpaceID }
    }
    // The old v1 shape needs Dockview to reconstruct its tabs. Keep the exact
    // legacy read/write path for this load; its normal first write produces v3,
    // which the next load can split without inventing a dock serialization.
    if (!legacy.stored && legacy.legacy) return {
      mode: 'legacy', activeSpaceID: null, legacy,
      problem: 'Spaces will be available after this browser finishes upgrading the saved layout. Your layout is still being saved.',
    }
    const source = legacy.stored
    const shell = {
      version: 1,
      rails: source?.rails ?? defaultRailPreferences(legacy.legacy?.sidebarWidth),
      ...(source?.expandedItems === undefined ? {} : { expandedItems: source.expandedItems }),
      ...(source?.knownWorkspaceItems === undefined ? {} : { knownWorkspaceItems: source.knownWorkspaceItems }),
    }
    const definition: StoredSpaceRecord = {
      version: 1,
      writeID: 'migration-v1',
      record: { id: mainSpaceID, name: 'main', order: 0, created: 0, updated: 0 },
    }
    const dock = { version: 4, dock: source?.dock ?? null }
    const definitionRaw = JSON.stringify(definition)
    const dockRaw = JSON.stringify(dock)
    const shellRaw = JSON.stringify(shell)
    verifiedWrite(storage, spaceBackupKey(mainSpaceID), definitionRaw)
    verifiedWrite(storage, spaceRecordKey(mainSpaceID), definitionRaw)
    verifiedWrite(storage, spaceLayoutBackupKey(mainSpaceID), dockRaw)
    verifiedWrite(storage, spaceLayoutKey(mainSpaceID), dockRaw)
    verifiedWrite(storage, shellStorageBackupKey, shellRaw)
    verifiedWrite(storage, shellStorageKey, shellRaw)
    verifiedWrite(storage, migrationMarkerKey, JSON.stringify({ version: 1, mainSpaceID }))
    return { mode: 'spaces', activeSpaceID: mainSpaceID }
  } catch {
    let legacy: LegacyLayoutFamilies = { stored: null, backup: null, legacy: null, recovering: false, lastGoodRaw: null }
    try { legacy = readLegacyLayoutFamilies(storage) } catch { /* storage remains unavailable */ }
    return {
      mode: 'legacy', activeSpaceID: null, legacy,
      problem: 'Spaces are unavailable in this browser right now. Your current layout is still being saved.',
    }
  }
}

export function selectActiveSpace(spaces: SpaceDefinition[], sessionID: string | null, lastActiveID: string | null) {
  const live = spaces.filter((space) => !space.deleted).sort((left, right) => left.order - right.order || left.id.localeCompare(right.id))
  return live.find((space) => space.id === sessionID)?.id ?? live.find((space) => space.id === lastActiveID)?.id ?? live[0]?.id ?? null
}

export function readActiveSpace(spaces: SpaceDefinition[], session: Pick<Storage, 'getItem'>, local: Pick<Storage, 'getItem'>) {
  try { return selectActiveSpace(spaces, session.getItem(activeSpaceSessionKey), local.getItem(lastActiveSpaceKey)) }
  catch { return selectActiveSpace(spaces, null, null) }
}

export function writeActiveSpace(id: string, session: Pick<Storage, 'setItem'>, local: Pick<Storage, 'setItem'>) {
  let persistent = true
  try { session.setItem(activeSpaceSessionKey, id) } catch { persistent = false }
  try { local.setItem(lastActiveSpaceKey, id) } catch { persistent = false }
  return persistent
}

type LayoutRecovery = {
  version: 1
  kind: 'closed' | 'corrupt'
  primaryRaw: string | null
  backupRaw: string | null
  updated: number
}

function parseRecovery(raw: string | null): LayoutRecovery | null {
  try {
    const value = JSON.parse(raw ?? '') as Partial<LayoutRecovery>
    if (value.version !== 1 || (value.kind !== 'closed' && value.kind !== 'corrupt') ||
      (value.primaryRaw !== null && typeof value.primaryRaw !== 'string') ||
      (value.backupRaw !== null && typeof value.backupRaw !== 'string') || typeof value.updated !== 'number') return null
    return value as LayoutRecovery
  } catch {
    return null
  }
}

export type SpaceResult<T> = { ok: true, value: T } | { ok: false, reason: string }

export function closeSpaceLayout(storage: MigrationStorage, id: string, now = Date.now()): SpaceResult<void> {
  try {
    const recoveryKey = layoutRecoveryKey(id)
    const existing = parseRecovery(storage.getItem(recoveryKey))
    const recovery: LayoutRecovery = {
      version: 1,
      kind: 'closed',
      primaryRaw: storage.getItem(spaceLayoutKey(id)) ?? existing?.primaryRaw ?? null,
      backupRaw: storage.getItem(spaceLayoutBackupKey(id)) ?? existing?.backupRaw ?? null,
      updated: now,
    }
    const raw = JSON.stringify(recovery)
    verifiedWrite(storage, recoveryKey, raw)
    storage.removeItem(spaceLayoutKey(id))
    storage.removeItem(spaceLayoutBackupKey(id))
    return { ok: true, value: undefined }
  } catch {
    return { ok: false, reason: 'This space could not be closed because its layout could not be kept for recovery. Nothing was discarded.' }
  }
}

export function reopenSpaceLayout(storage: MigrationStorage, id: string): SpaceResult<void> {
  try {
    const recovery = parseRecovery(storage.getItem(layoutRecoveryKey(id)))
    if (!recovery) return { ok: false, reason: 'This recently closed layout is no longer available.' }
    if (recovery.backupRaw !== null) verifiedWrite(storage, spaceLayoutBackupKey(id), recovery.backupRaw)
    if (recovery.primaryRaw !== null) verifiedWrite(storage, spaceLayoutKey(id), recovery.primaryRaw)
    else verifiedWrite(storage, spaceLayoutKey(id), JSON.stringify({ version: 4, dock: null }))
    return { ok: true, value: undefined }
  } catch {
    return { ok: false, reason: 'This space could not be reopened because its layout could not be restored. The recovery copy was kept.' }
  }
}

export function removeLayoutRecovery(storage: Pick<Storage, 'removeItem'>, id: string) {
  try { storage.removeItem(layoutRecoveryKey(id)) } catch { /* best effort; next purge retries */ }
}

export function allSpacesStorageKeys(storage: Pick<Storage, 'length' | 'key'>) {
  const prefixes = [spacesRecordPrefix, spacesBackupPrefix, spacesRecoveryPrefix, spaceLayoutPrefix, spaceLayoutBackupPrefix, spaceLayoutRecoveryPrefix]
  return Array.from({ length: storage.length }, (_, index) => storage.key(index)).flatMap((key) => key && prefixes.some((prefix) => key.startsWith(prefix)) ? [key] : [])
}

export function clearAllLayoutFamilies(storage: Pick<Storage, 'length' | 'key' | 'removeItem'>) {
  for (const key of allSpacesStorageKeys(storage)) storage.removeItem(key)
  for (const key of [shellStorageKey, shellStorageBackupKey, migrationMarkerKey, lastActiveSpaceKey,
    layoutStorageKey, layoutStorageBackupKey, v2LayoutStorageKey, legacyLayoutStorageKey]) storage.removeItem(key)
}
