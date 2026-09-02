import { getState, upsertState } from '../../api/client.ts'
import {
  createStateSync,
  createStateSyncPersistence,
  type GenericStateRow,
  type StateSyncMessages,
  type StateSyncOptions,
  type StateSyncPersistence,
  type StateSyncStore,
  type StateTransport,
} from '../../shared/stateSync.ts'
import { compareStateVersions } from '../../shared/stateVersion.ts'
import type { Note, NoteSource, StoredNoteRecord } from './notesStore.ts'

export const browserOnlyNotesMessage = 'Notes are saved in this browser only.'

function noteName(rows: GenericStateRow[]) {
  for (const row of rows) {
    if (row.deleted || !row.value || typeof row.value !== 'object') continue
    const value = row.value as { text?: unknown, quote?: unknown }
    const text = [value.text, value.quote].find((candidate): candidate is string => typeof candidate === 'string' && candidate.trim().length > 0)
    if (!text) continue
    const words = text.trim().split(/\s+/).slice(0, 6).join(' ')
    return words.length > 48 ? `${words.slice(0, 47)}…` : words
  }
  return 'Untitled note'
}

export const notesSyncMessages: StateSyncMessages = {
  browserOnly: browserOnlyNotesMessage,
  pending: (count) => `Notes are saved on this device, but ${count} ${count === 1 ? 'change' : 'changes'} could not sync. ${count === 1 ? 'It' : 'They'} will retry automatically.`,
  queuePersistence: 'Notes are saved on this device, but the pending sync queue could not be saved between browser sessions.',
  postRefused: (rows) => `Note '${noteName(rows)}' is too large to sync; it stays on this device.`,
  cursorPersistence: 'Notes are saved on this device, but the sync cursor could not be saved between browser sessions.',
}

type NotesSyncOptions = Omit<StateSyncOptions, 'namespace' | 'compare' | 'messages'>

export function createNotesSync(options: NotesSyncOptions) {
  return createStateSync({
    ...options,
    namespace: 'notes',
    compare: (left, right) => compareStateVersions(left.updated, left.writeID, right.updated, right.writeID),
    messages: notesSyncMessages,
  })
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>

export function createNotesSyncPersistence(storage: StorageLike): StateSyncPersistence {
  return createStateSyncPersistence(storage, 'notes')
}

function validSource(value: unknown): value is NoteSource {
  if (!value || typeof value !== 'object') return false
  const source = value as Partial<NoteSource>
  if (source.kind === 'transcript') return typeof source.agent === 'string'
  if (source.kind !== 'file' && source.kind !== 'diff') return false
  if (typeof source.path !== 'string') return false
  if (source.start !== undefined && (typeof source.start !== 'number' || !Number.isFinite(source.start))) return false
  if (source.end !== undefined && (typeof source.end !== 'number' || !Number.isFinite(source.end))) return false
  return source.kind === 'file' || typeof (source as { base?: unknown }).base === 'string'
}

export function storedNoteToStateRow(stored: StoredNoteRecord): GenericStateRow {
  if ('deleted' in stored.record) {
    const value = { id: stored.record.id }
    const { updated } = stored.record
    return { key: stored.record.id, value, updated, writeID: stored.writeID, deleted: true }
  }
  const { updated, ...value } = stored.record
  return { key: stored.record.id, value, updated, writeID: stored.writeID, deleted: false }
}

export function stateRowToStoredNote(row: GenericStateRow): StoredNoteRecord | null {
  if (!row.value || typeof row.value !== 'object' || typeof row.updated !== 'number' || !Number.isFinite(row.updated) ||
    typeof row.writeID !== 'string' || !row.writeID || (row.deleted !== false && row.deleted !== true)) return null
  const value = row.value as Partial<Note>
  if (value.id !== row.key) return null
  if (row.deleted) return { version: 1, writeID: row.writeID, record: { id: row.key, deleted: true, updated: row.updated } }
  if (typeof value.group !== 'string' || typeof value.text !== 'string' || typeof value.created !== 'number' || !Number.isFinite(value.created) ||
    (value.quote !== undefined && typeof value.quote !== 'string') || (value.source !== undefined && !validSource(value.source))) return null
  return {
    version: 1,
    writeID: row.writeID,
    record: {
      id: row.key,
      group: value.group,
      text: value.text,
      ...(value.quote === undefined ? {} : { quote: value.quote }),
      ...(value.source === undefined ? {} : { source: value.source }),
      created: value.created,
      updated: row.updated,
    },
  }
}

export function notesStoreSyncAdapter(store: {
  records: () => StoredNoteRecord[]
  merge: (records: StoredNoteRecord[]) => void
  subscribeMutations: (listener: (records: StoredNoteRecord[]) => void) => () => void
  list: () => Array<{ id: string }>
}): StateSyncStore {
  return {
    all: () => store.records().map(storedNoteToStateRow),
    merge: (rows) => store.merge(rows.flatMap((row) => {
      const stored = stateRowToStoredNote(row)
      return stored ? [stored] : []
    })),
    liveIDs: () => store.list().map(({ id }) => id),
    subscribeMutations: (listener) => store.subscribeMutations((records) => listener(records.map(storedNoteToStateRow))),
  }
}

export function browserNotesTransport(): StateTransport {
  return {
    since: async (rev) => {
      try { return await getState('notes', rev) } catch (error) {
        if (error instanceof Error && 'response' in error && (error.response as Response | undefined)?.status === 404) return { rows: [], rev: 0 }
        throw error
      }
    },
    upsert: (rows) => upsertState('notes', rows),
  }
}
