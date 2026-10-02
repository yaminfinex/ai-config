import { getState, upsertState } from '../../api/client.ts'
import {
  createStateSync,
  createStateSyncPersistence,
  type StateSyncMessages,
  type StateSyncOptions,
  type StateSyncPersistence,
  type StateSyncStore,
  type StateTransport,
} from '../../shared/stateSync.ts'
import { compareStateVersions } from '../../shared/stateVersion.ts'
import type { ReadMarkerStore } from './readMarkerStore.ts'

export const readMarkersNamespace = 'read.markers'

export const readMarkersSyncMessages: StateSyncMessages = {
  browserOnly: 'What you have read is saved in this browser only.',
  pending: (count) => `What you have read is saved on this device, but ${count} ${count === 1 ? 'change' : 'changes'} could not sync. ${count === 1 ? 'It' : 'They'} will retry automatically.`,
  queuePersistence: 'What you have read is saved on this device, but the pending sync queue could not be saved between browser sessions.',
  postRefused: (_rows, detail) => `What you have read is saved on this device, but the server refused to sync it: ${detail}`,
  cursorPersistence: 'What you have read is saved on this device, but the sync cursor could not be saved between browser sessions.',
}

type ReadMarkersSyncOptions = Omit<StateSyncOptions, 'namespace' | 'compare' | 'messages'>

export function createReadMarkersSync(options: ReadMarkersSyncOptions) {
  return createStateSync({
    ...options,
    namespace: readMarkersNamespace,
    compare: (left, right) => compareStateVersions(left.updated, left.writeID, right.updated, right.writeID),
    messages: readMarkersSyncMessages,
  })
}

export function createReadMarkersSyncPersistence(storage: Pick<Storage, 'getItem' | 'setItem'>): StateSyncPersistence {
  return createStateSyncPersistence(storage, readMarkersNamespace)
}

export function readMarkerStoreSyncAdapter(store: ReadMarkerStore): StateSyncStore {
  return {
    all: store.rows,
    merge: store.merge,
    liveIDs: () => Object.keys(store.markers()),
    subscribeMutations: store.subscribeMutations,
  }
}

export function browserReadMarkersTransport(): StateTransport {
  return {
    since: async (rev) => {
      try { return await getState(readMarkersNamespace, rev) } catch (error) {
        if (error instanceof Error && 'response' in error && (error.response as Response | undefined)?.status === 404) return { rows: [], rev: 0 }
        throw error
      }
    },
    upsert: (rows) => upsertState(readMarkersNamespace, rows),
  }
}
