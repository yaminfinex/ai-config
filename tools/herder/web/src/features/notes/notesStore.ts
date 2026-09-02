import { compareStateVersions } from '../../shared/stateVersion.ts'

export const notesStoragePrefix = 'herder.web.notes.v1:'

const recordPrefix = `${notesStoragePrefix}record:`
const backupPrefix = `${notesStoragePrefix}last-good:`
const recoveryPrefix = `${notesStoragePrefix}recovery:`
const defaultTombstoneRetention = 30 * 24 * 60 * 60 * 1_000

export type NoteSource =
  | { kind: 'file', path: string, start?: number, end?: number }
  | { kind: 'diff', path: string, base: string, start?: number, end?: number }
  | { kind: 'transcript', agent: string }

export type Note = {
  id: string
  group: string
  text: string
  quote?: string
  source?: NoteSource
  created: number
  updated: number
}

export type NoteTombstone = { id: string, deleted: true, updated: number }
export type NoteRecord = Note | NoteTombstone
export type StoredNoteRecord = { version: 1, writeID: string, record: NoteRecord }

export type NotesStorage = Pick<Storage, 'length' | 'key' | 'getItem' | 'setItem' | 'removeItem'>

export type NotesStoreEventTarget = {
  addEventListener: (type: 'storage' | 'pagehide', listener: (event: StorageEvent | PageTransitionEvent) => void) => void
  removeEventListener: (type: 'storage' | 'pagehide', listener: (event: StorageEvent | PageTransitionEvent) => void) => void
}

type Success<T> = { ok: true, value: T }
type Refusal = { ok: false, reason: string }
export type NotesResult<T> = Success<T> | Refusal
export type NotesStatus = { persistent: boolean, recovered: boolean, problem: string }

export type NotesStore = {
  list: () => Note[]
  listGroup: (group: string) => Note[]
  add: (input: { group: string, text: string, quote?: string, source?: NoteSource }) => NotesResult<Note>
  edit: (id: string, changes: { text?: string, group?: string }, fallback?: Note) => NotesResult<Note>
  delete: (ids: string[]) => NotesResult<number>
  records: () => StoredNoteRecord[]
  merge: (records: StoredNoteRecord[]) => void
  subscribeMutations: (listener: (records: StoredNoteRecord[]) => void) => () => void
  subscribe: (listener: () => void) => () => void
  status: () => NotesStatus
  flush: () => boolean
  dispose: () => void
}

type Options = {
  storage?: NotesStorage | null
  events?: NotesStoreEventTarget | null
  now?: () => number
  randomID?: () => string
  schedule?: (callback: () => void, delay: number) => unknown
  cancel?: (handle: unknown) => void
  debounceMs?: number
  maxActiveNotes?: number
  maxNoteBytes?: number
  maxTotalBytes?: number
  tombstoneRetentionMs?: number
}

function browserStorage(): NotesStorage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

function browserEvents(): NotesStoreEventTarget | null {
  return typeof window === 'undefined' ? null : window
}

function parseStored(raw: string | null): StoredNoteRecord | null {
  if (!raw) return null
  try {
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== 'object') return null
    const stored = value as Partial<StoredNoteRecord>
    if (stored.version !== 1 || typeof stored.writeID !== 'string' || !stored.record || typeof stored.record !== 'object') return null
    const record = stored.record as Partial<NoteRecord>
    if (typeof record.id !== 'string' || typeof record.updated !== 'number' || !Number.isFinite(record.updated)) return null
    if ('deleted' in record) return record.deleted === true ? stored as StoredNoteRecord : null
    const note = record as Partial<Note>
    if (typeof note.group !== 'string' || typeof note.text !== 'string' || typeof note.created !== 'number' || !Number.isFinite(note.created)) return null
    if (note.quote !== undefined && typeof note.quote !== 'string') return null
    return stored as StoredNoteRecord
  } catch {
    return null
  }
}

function newer(left: StoredNoteRecord, right: StoredNoteRecord) {
  return compareStateVersions(left.record.updated, left.writeID, right.record.updated, right.writeID) >= 0 ? left : right
}

function active(record: NoteRecord): record is Note {
  return !('deleted' in record)
}

function noteBytes(note: Pick<Note, 'text' | 'quote'>) {
  return new TextEncoder().encode(`${note.quote ?? ''}${note.text}`).byteLength
}

function storedBytes(records: Iterable<StoredNoteRecord>) {
  let total = 0
  for (const record of records) total += new TextEncoder().encode(JSON.stringify(record)).byteLength
  return total
}

// crypto.randomUUID only exists in secure contexts; the owner browses over
// plain http, so fall back to a getRandomValues-built UUIDv4 there.
function fallbackUUID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export function defaultRandomID(): string {
  return typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : fallbackUUID()
}

function recordKey(id: string) { return `${recordPrefix}${encodeURIComponent(id)}` }
function backupKey(id: string) { return `${backupPrefix}${encodeURIComponent(id)}` }
function recordID(key: string) {
  try { return decodeURIComponent(key.slice(recordPrefix.length)) }
  catch { return null }
}

export function createNotesStore(options: Options = {}): NotesStore {
  const now = options.now ?? Date.now
  const randomID = options.randomID ?? defaultRandomID
  const schedule = options.schedule ?? ((callback, delay) => window.setTimeout(callback, delay))
  const cancel = options.cancel ?? ((handle) => window.clearTimeout(handle as number))
  const debounceMs = options.debounceMs ?? 120
  const maxActiveNotes = options.maxActiveNotes ?? 100
  const maxNoteBytes = options.maxNoteBytes ?? 8 * 1_024
  const maxTotalBytes = options.maxTotalBytes ?? 100 * 1_024
  const tombstoneRetentionMs = options.tombstoneRetentionMs ?? defaultTombstoneRetention
  let storage = options.storage === undefined ? browserStorage() : options.storage
  const events = options.events === undefined ? browserEvents() : options.events
  let storeStatus: NotesStatus = storage
    ? { persistent: true, recovered: false, problem: '' }
    : { persistent: false, recovered: false, problem: 'Notes are kept for this session but are not saved between browser sessions.' }
  const records = new Map<string, StoredNoteRecord>()
  const dirty = new Set<string>()
  const listeners = new Set<() => void>()
  const mutationListeners = new Set<(records: StoredNoteRecord[]) => void>()
  let timer: unknown
  let eventsAttached = false

  const notify = () => { for (const listener of listeners) listener() }
  // A thrown exception here would vanish inside a click handler and the owner
  // would see nothing happen; every unexpected failure must become a visible refusal.
  const guarded = <T,>(operation: () => NotesResult<T>): NotesResult<T> => {
    try {
      return operation()
    } catch {
      return { ok: false, reason: 'Something went wrong in this browser and the change was not saved. Existing notes were left untouched.' }
    }
  }
  const degrade = () => {
    storage = null
    dirty.clear()
    if (timer !== undefined) cancel(timer)
    timer = undefined
    storeStatus = { ...storeStatus, persistent: false, problem: 'Notes are kept for this session but are not saved between browser sessions.' }
    notify()
  }
  const preserveRaw = (id: string, raw: string) => {
    if (!storage) return
    try {
      storage.setItem(`${recoveryPrefix}${encodeURIComponent(id)}`, raw)
    } catch {
      degrade()
    }
  }
  const recoverRecord = (id: string, raw: string | null) => {
    const primary = parseStored(raw)
    if (primary) return primary
    if (raw) preserveRaw(id, raw)
    if (!storage) return null
    try {
      const backup = parseStored(storage.getItem(backupKey(id)))
      if (backup) {
        storeStatus = { ...storeStatus, recovered: true, problem: 'Some notes were restored from their last good copies. The unreadable data was kept for recovery.' }
        return backup
      }
      if (raw) storeStatus = { ...storeStatus, recovered: true, problem: 'One unreadable note was kept for recovery. No usable copy was available.' }
    } catch {
      degrade()
    }
    return null
  }
  const load = () => {
    if (!storage) return
    try {
      const keys = Array.from({ length: storage.length }, (_, index) => storage?.key(index) ?? null)
      for (const key of keys) {
        if (!key?.startsWith(recordPrefix)) continue
        const id = recordID(key)
        if (id === null) continue
        const stored = recoverRecord(id, storage.getItem(key))
        if (!stored) continue
        if (!active(stored.record) && now() - stored.record.updated > tombstoneRetentionMs) {
          storage.removeItem(key)
          storage.removeItem(backupKey(id))
          continue
        }
        const current = records.get(id)
        records.set(id, current ? newer(current, stored) : stored)
      }
    } catch {
      degrade()
    }
  }

  const currentNotes = () => [...records.values()]
    .flatMap((stored) => active(stored.record) ? [stored.record] : [])
    .sort((left, right) => right.updated - left.updated || left.id.localeCompare(right.id))

  const totalWith = (candidate: StoredNoteRecord) => storedBytes([...records.values()].filter((stored) => stored.record.id !== candidate.record.id).concat(candidate))
  const refuseCandidate = (candidate: StoredNoteRecord, adding: boolean): string => {
    if (active(candidate.record) && noteBytes(candidate.record) > maxNoteBytes) return 'This note is too long to save. Shorten it and try again.'
    if (adding && currentNotes().length >= maxActiveNotes) return `There is no room for another note. The ${maxActiveNotes}-note limit refuses new writing rather than deleting older notes.`
    if (totalWith(candidate) > maxTotalBytes) return 'There is no room for this note in browser storage. Existing notes were left untouched.'
    return ''
  }
  const scheduleFlush = () => {
    if (!storage || timer !== undefined) return
    timer = schedule(() => { timer = undefined; flush() }, debounceMs)
  }
  const putBatch = (values: StoredNoteRecord[]) => {
    for (const stored of values) {
      records.set(stored.record.id, stored)
      dirty.add(stored.record.id)
    }
    scheduleFlush()
    notify()
    for (const listener of mutationListeners) listener(values)
  }
  const put = (stored: StoredNoteRecord) => putBatch([stored])
  const reconcile = (id: string) => {
    if (!storage) return records.get(id)
    try {
      const persisted = recoverRecord(id, storage.getItem(recordKey(id)))
      const memory = records.get(id)
      if (!persisted) return memory
      const winner = memory ? newer(memory, persisted) : persisted
      records.set(id, winner)
      if (winner !== memory) notify()
      return winner
    } catch {
      degrade()
      return records.get(id)
    }
  }
  const flush = () => {
    if (!storage || dirty.size === 0) return false
    let wrote = false
    for (const id of [...dirty]) {
      try {
        const key = recordKey(id)
        const raw = storage.getItem(key)
        const persisted = recoverRecord(id, raw)
        const memory = records.get(id)
        if (!memory) { dirty.delete(id); continue }
        const winner = persisted ? newer(memory, persisted) : memory
        records.set(id, winner)
        if (winner !== memory) { dirty.delete(id); notify(); continue }
        const nextRaw = JSON.stringify(memory)
        if (!active(memory.record)) storage.removeItem(backupKey(id))
        else if (raw && parseStored(raw)) storage.setItem(backupKey(id), raw)
        else storage.setItem(backupKey(id), nextRaw)
        storage.setItem(key, nextRaw)
        dirty.delete(id)
        wrote = true
      } catch {
        degrade()
        return false
      }
    }
    return wrote
  }
  const onEvent = (event: StorageEvent | PageTransitionEvent) => {
    if (event.type === 'pagehide') { flush(); return }
    const storageEvent = event as StorageEvent
    if (!storageEvent.key?.startsWith(notesStoragePrefix) || !storageEvent.key.startsWith(recordPrefix)) return
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
  const storageListener = (event: StorageEvent | PageTransitionEvent) => onEvent(event)
  const pagehideListener = (event: StorageEvent | PageTransitionEvent) => onEvent(event)
  const attachEvents = () => {
    if (eventsAttached) return
    eventsAttached = true
    events?.addEventListener('storage', storageListener)
    events?.addEventListener('pagehide', pagehideListener)
  }
  const detachEvents = () => {
    if (!eventsAttached) return
    eventsAttached = false
    events?.removeEventListener('storage', storageListener)
    events?.removeEventListener('pagehide', pagehideListener)
  }

  load()

  return {
    list: currentNotes,
    listGroup: (group) => currentNotes().filter((note) => note.group === group),
    add: ({ group, text, quote, source }) => guarded(() => {
      const value = text.trim()
      const captured = quote?.trim()
      if (!value && !captured) return { ok: false, reason: 'Write something before saving this note.' }
      const timestamp = now()
      const note: Note = { id: randomID(), group, text: value, ...(captured ? { quote: captured } : {}), ...(source ? { source } : {}), created: timestamp, updated: timestamp }
      const stored: StoredNoteRecord = { version: 1, writeID: randomID(), record: note }
      const reason = refuseCandidate(stored, true)
      if (reason) return { ok: false, reason }
      put(stored)
      return { ok: true, value: note }
    }),
    edit: (id, changes, fallback) => guarded(() => {
      const current = reconcile(id)
      const base = current && active(current.record) ? current.record : fallback?.id === id ? fallback : null
      if (!base) return { ok: false, reason: 'This note no longer exists.' }
      const text = changes.text === undefined ? base.text : changes.text.trim()
      if (!text && !base.quote) return { ok: false, reason: 'A note cannot be empty.' }
      const updated = Math.max(now(), (current?.record.updated ?? base.updated) + 1)
      const note: Note = { ...base, ...(changes.group === undefined ? {} : { group: changes.group }), text, updated }
      const stored: StoredNoteRecord = { version: 1, writeID: randomID(), record: note }
      const reason = refuseCandidate(stored, false)
      if (reason) return { ok: false, reason }
      put(stored)
      return { ok: true, value: note }
    }),
    delete: (ids) => guarded(() => {
      const tombstones: StoredNoteRecord[] = []
      for (const id of ids) {
        const current = reconcile(id)
        if (!current || !active(current.record)) continue
        tombstones.push({ version: 1, writeID: randomID(), record: { id, deleted: true, updated: Math.max(now(), current.record.updated + 1) } })
      }
      if (tombstones.length === 0) return { ok: false, reason: 'These notes no longer exist.' }
      putBatch(tombstones)
      return { ok: true, value: tombstones.length }
    }),
    records: () => [...records.values()],
    merge: (values) => {
      let changed = false
      for (const incoming of values) {
        const current = records.get(incoming.record.id)
        if (current && newer(current, incoming) === current) continue
        records.set(incoming.record.id, incoming)
        dirty.add(incoming.record.id)
        changed = true
      }
      if (!changed) return
      scheduleFlush()
      notify()
    },
    subscribeMutations: (listener) => {
      mutationListeners.add(listener)
      return () => mutationListeners.delete(listener)
    },
    subscribe: (listener) => {
      listeners.add(listener)
      attachEvents()
      return () => listeners.delete(listener)
    },
    status: () => storeStatus,
    flush,
    dispose: () => {
      flush()
      if (timer !== undefined) cancel(timer)
      timer = undefined
      detachEvents()
      listeners.clear()
      mutationListeners.clear()
    },
  }
}
