import { defaultRandomID } from '../notes/notesStore.ts'
import {
  spaceBackupKey,
  spaceRecordKey,
  spaceRecoveryKey,
  spacesRecordPrefix,
  type SpaceDefinition,
  type SpaceResult,
  type StoredSpaceRecord,
} from './spacesModel.ts'

export const defaultMaxSpaces = 16
const defaultTombstoneRetention = 30 * 24 * 60 * 60 * 1_000

export type SpacesStorage = Pick<Storage, 'length' | 'key' | 'getItem' | 'setItem' | 'removeItem'>
export type SpacesStoreEventTarget = {
  addEventListener: (type: 'storage' | 'pagehide', listener: (event: StorageEvent | PageTransitionEvent) => void) => void
  removeEventListener: (type: 'storage' | 'pagehide', listener: (event: StorageEvent | PageTransitionEvent) => void) => void
}
export type SpacesStatus = { persistent: boolean, recovered: boolean, problem: string }
export type SpacesStore = {
  list: () => SpaceDefinition[]
  recentlyClosed: () => SpaceDefinition[]
  create: () => SpaceResult<SpaceDefinition>
  rename: (id: string, name: string) => SpaceResult<SpaceDefinition>
  reorder: (id: string, targetIndex: number) => SpaceResult<SpaceDefinition>
  close: (id: string) => SpaceResult<SpaceDefinition>
  reopen: (id: string) => SpaceResult<SpaceDefinition>
  rollbackCreate: (id: string) => boolean
  subscribe: (listener: () => void) => () => void
  status: () => SpacesStatus
  flush: () => boolean
  dispose: () => void
}

type Options = {
  storage?: SpacesStorage | null
  events?: SpacesStoreEventTarget | null
  now?: () => number
  randomID?: () => string
  schedule?: (callback: () => void, delay: number) => unknown
  cancel?: (handle: unknown) => void
  debounceMs?: number
  maxSpaces?: number
  tombstoneRetentionMs?: number
  onPurge?: (id: string) => void
}

function browserStorage(): SpacesStorage | null {
  try { return window.localStorage } catch { return null }
}

function browserEvents(): SpacesStoreEventTarget | null {
  return typeof window === 'undefined' ? null : window
}

function parseStored(raw: string | null): StoredSpaceRecord | null {
  if (!raw) return null
  try {
    const value = JSON.parse(raw) as Partial<StoredSpaceRecord>
    const item = value.record as Partial<SpaceDefinition> | undefined
    if (value.version !== 1 || typeof value.writeID !== 'string' || !item || typeof item.id !== 'string' ||
      typeof item.name !== 'string' || typeof item.order !== 'number' || !Number.isFinite(item.order) ||
      typeof item.created !== 'number' || !Number.isFinite(item.created) ||
      typeof item.updated !== 'number' || !Number.isFinite(item.updated) ||
      (item.deleted !== undefined && item.deleted !== true)) return null
    return value as StoredSpaceRecord
  } catch {
    return null
  }
}

function newer(left: StoredSpaceRecord, right: StoredSpaceRecord) {
  if (left.record.updated !== right.record.updated) return left.record.updated > right.record.updated ? left : right
  return left.writeID >= right.writeID ? left : right
}

function recordID(key: string) {
  try { return decodeURIComponent(key.slice(spacesRecordPrefix.length)) } catch { return null }
}

export function createSpacesStore(options: Options = {}): SpacesStore {
  const now = options.now ?? Date.now
  const randomID = options.randomID ?? defaultRandomID
  const schedule = options.schedule ?? ((callback, delay) => window.setTimeout(callback, delay))
  const cancel = options.cancel ?? ((handle) => window.clearTimeout(handle as number))
  const debounceMs = options.debounceMs ?? 120
  const maxSpaces = options.maxSpaces ?? defaultMaxSpaces
  const tombstoneRetentionMs = options.tombstoneRetentionMs ?? defaultTombstoneRetention
  let storage = options.storage === undefined ? browserStorage() : options.storage
  const events = options.events === undefined ? browserEvents() : options.events
  let storeStatus: SpacesStatus = storage
    ? { persistent: true, recovered: false, problem: '' }
    : { persistent: false, recovered: false, problem: 'Spaces are kept for this session but are not saved between browser sessions.' }
  const records = new Map<string, StoredSpaceRecord>()
  const dirty = new Set<string>()
  const listeners = new Set<() => void>()
  let timer: unknown
  let attached = false

  const notify = () => { for (const listener of listeners) listener() }
  const degrade = () => {
    storage = null
    dirty.clear()
    if (timer !== undefined) cancel(timer)
    timer = undefined
    storeStatus = { ...storeStatus, persistent: false, problem: 'Spaces are kept for this session but are not saved between browser sessions.' }
    notify()
  }
  const preserveRaw = (id: string, raw: string) => {
    if (!storage) return
    try { storage.setItem(spaceRecoveryKey(id), raw) } catch { degrade() }
  }
  const recoverRecord = (id: string, raw: string | null) => {
    const primary = parseStored(raw)
    if (primary) return primary
    if (raw) preserveRaw(id, raw)
    if (!storage) return null
    try {
      const backup = parseStored(storage.getItem(spaceBackupKey(id)))
      if (backup) {
        storeStatus = { persistent: true, recovered: true, problem: 'Some spaces were restored from their last good copies. The unreadable data was kept for recovery.' }
        return backup
      }
      if (raw) storeStatus = { persistent: true, recovered: true, problem: 'One unreadable space was kept for recovery. No usable copy was available.' }
    } catch { degrade() }
    return null
  }
  const load = () => {
    if (!storage) return
    try {
      const keys = Array.from({ length: storage.length }, (_, index) => storage?.key(index) ?? null)
      for (const key of keys) {
        if (!key?.startsWith(spacesRecordPrefix)) continue
        const id = recordID(key)
        if (id === null) continue
        const stored = recoverRecord(id, storage.getItem(key))
        if (!stored) continue
        if (stored.record.deleted && now() - stored.record.updated > tombstoneRetentionMs) {
          storage.removeItem(key)
          storage.removeItem(spaceBackupKey(id))
          options.onPurge?.(id)
          continue
        }
        const current = records.get(id)
        records.set(id, current ? newer(current, stored) : stored)
      }
    } catch { degrade() }
  }
  const live = () => [...records.values()].flatMap(({ record }) => record.deleted ? [] : [record])
    .sort((left, right) => left.order - right.order || left.id.localeCompare(right.id))
  const closed = () => [...records.values()].flatMap(({ record }) => record.deleted ? [record] : [])
    .sort((left, right) => right.updated - left.updated || left.id.localeCompare(right.id))
  const scheduleFlush = () => {
    if (!storage || timer !== undefined) return
    timer = schedule(() => { timer = undefined; flush() }, debounceMs)
  }
  const put = (stored: StoredSpaceRecord) => {
    records.set(stored.record.id, stored)
    dirty.add(stored.record.id)
    scheduleFlush()
    notify()
  }
  const putBatch = (values: StoredSpaceRecord[]) => {
    for (const stored of values) {
      records.set(stored.record.id, stored)
      dirty.add(stored.record.id)
    }
    scheduleFlush()
    notify()
  }
  const reconcile = (id: string) => {
    if (!storage) return records.get(id)
    try {
      const persisted = recoverRecord(id, storage.getItem(spaceRecordKey(id)))
      const memory = records.get(id)
      if (!persisted) return memory
      const winner = memory ? newer(memory, persisted) : persisted
      records.set(id, winner)
      if (winner !== memory) notify()
      return winner
    } catch { degrade(); return records.get(id) }
  }
  const flush = () => {
    if (!storage || dirty.size === 0) return false
    let wrote = false
    for (const id of [...dirty]) {
      try {
        const key = spaceRecordKey(id)
        const raw = storage.getItem(key)
        const persisted = recoverRecord(id, raw)
        const memory = records.get(id)
        if (!memory) { dirty.delete(id); continue }
        const winner = persisted ? newer(memory, persisted) : memory
        records.set(id, winner)
        if (winner !== memory) { dirty.delete(id); notify(); continue }
        const nextRaw = JSON.stringify(memory)
        if (memory.record.deleted) storage.removeItem(spaceBackupKey(id))
        else if (raw && parseStored(raw)) storage.setItem(spaceBackupKey(id), raw)
        else storage.setItem(spaceBackupKey(id), nextRaw)
        storage.setItem(key, nextRaw)
        dirty.delete(id)
        wrote = true
      } catch { degrade(); return false }
    }
    return wrote
  }
  const guarded = <T,>(operation: () => SpaceResult<T>): SpaceResult<T> => {
    try { return operation() } catch {
      return { ok: false, reason: 'Something went wrong in this browser and the change was not saved. Existing spaces were left untouched.' }
    }
  }
  const onEvent = (event: StorageEvent | PageTransitionEvent) => {
    if (event.type === 'pagehide') { flush(); return }
    const storageEvent = event as StorageEvent
    if (!storageEvent.key?.startsWith(spacesRecordPrefix)) return
    if (storageEvent.storageArea && storage && storageEvent.storageArea !== storage) return
    const id = recordID(storageEvent.key)
    if (id === null) return
    const incoming = recoverRecord(id, storageEvent.newValue)
    if (!incoming) return
    const current = records.get(id)
    const winner = current ? newer(current, incoming) : incoming
    if (winner === current) return
    records.set(id, winner)
    dirty.delete(id)
    notify()
  }
  const listener = (event: StorageEvent | PageTransitionEvent) => onEvent(event)
  const attach = () => {
    if (attached) return
    attached = true
    events?.addEventListener('storage', listener)
    events?.addEventListener('pagehide', listener)
  }
  const detach = () => {
    if (!attached) return
    attached = false
    events?.removeEventListener('storage', listener)
    events?.removeEventListener('pagehide', listener)
  }
  load()

  return {
    list: live,
    recentlyClosed: closed,
    create: () => guarded(() => {
      if (live().length >= maxSpaces) return { ok: false, reason: `There is no room for another space. The ${maxSpaces}-space limit refuses creation rather than closing an existing space.` }
      const usedNames = new Set([...records.values()].map(({ record }) => record.name))
      let number = 2
      while (usedNames.has(`space ${number}`)) number += 1
      const timestamp = now()
      const space: SpaceDefinition = {
        id: randomID(), name: `space ${number}`,
        order: Math.max(-1, ...live().map(({ order }) => order)) + 1,
        created: timestamp, updated: timestamp,
      }
      put({ version: 1, writeID: randomID(), record: space })
      return { ok: true, value: space }
    }),
    rename: (id, name) => guarded(() => {
      const current = reconcile(id)
      if (!current || current.record.deleted) return { ok: false, reason: 'This space is no longer open.' }
      const value = name.trim()
      if (!value) return { ok: false, reason: 'A space needs a name.' }
      const space = { ...current.record, name: value, updated: Math.max(now(), current.record.updated + 1) }
      put({ version: 1, writeID: randomID(), record: space })
      return { ok: true, value: space }
    }),
    reorder: (id, targetIndex) => guarded(() => {
      for (const space of live()) reconcile(space.id)
      const ordered = live()
      const sourceIndex = ordered.findIndex((space) => space.id === id)
      if (sourceIndex < 0) return { ok: false, reason: 'This space is no longer open.' }
      const destination = Math.max(0, Math.min(Math.trunc(targetIndex), ordered.length - 1))
      if (destination === sourceIndex) return { ok: true, value: ordered[sourceIndex] }
      const [moved] = ordered.splice(sourceIndex, 1)
      ordered.splice(destination, 0, moved)
      const updated = Math.max(now(), ...ordered.map((space) => space.updated + 1))
      const writeID = randomID()
      const batch = ordered.map((space, order): StoredSpaceRecord => ({
        version: 1, writeID, record: { ...space, order, updated },
      }))
      putBatch(batch)
      return { ok: true, value: batch[destination].record }
    }),
    close: (id) => guarded(() => {
      const current = reconcile(id)
      if (!current || current.record.deleted) return { ok: false, reason: 'This space is already closed.' }
      const space: SpaceDefinition = { ...current.record, deleted: true, updated: Math.max(now(), current.record.updated + 1) }
      put({ version: 1, writeID: randomID(), record: space })
      return { ok: true, value: space }
    }),
    reopen: (id) => guarded(() => {
      if (live().length >= maxSpaces) return { ok: false, reason: `This space cannot be reopened while the ${maxSpaces}-space limit is full.` }
      const current = reconcile(id)
      if (!current || !current.record.deleted) return { ok: false, reason: 'This space is not recently closed.' }
      const { deleted: _deleted, ...rest } = current.record
      void _deleted
      const space: SpaceDefinition = {
        ...rest,
        order: Math.max(-1, ...live().map(({ order }) => order)) + 1,
        updated: Math.max(now(), current.record.updated + 1),
      }
      put({ version: 1, writeID: randomID(), record: space })
      return { ok: true, value: space }
    }),
    rollbackCreate: (id) => {
      const current = records.get(id)
      if (!current || current.record.deleted || !dirty.has(id)) return false
      records.delete(id)
      dirty.delete(id)
      if (dirty.size === 0 && timer !== undefined) {
        cancel(timer)
        timer = undefined
      }
      notify()
      return true
    },
    subscribe: (subscriber) => {
      listeners.add(subscriber)
      attach()
      return () => {
        listeners.delete(subscriber)
        if (listeners.size === 0) detach()
      }
    },
    status: () => storeStatus,
    flush,
    dispose: () => {
      flush()
      if (timer !== undefined) cancel(timer)
      timer = undefined
      detach()
      listeners.clear()
    },
  }
}
