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
import type { SpaceMembersStore } from './spaceMembersStore.ts'

export const spaceMembersNamespace = 'spaces.members'

export const spaceMembersSyncMessages: StateSyncMessages = {
  browserOnly: 'Space tabs are saved in this browser only.',
  pending: (count) => `Space tabs are saved on this device, but ${count} ${count === 1 ? 'change' : 'changes'} could not sync. ${count === 1 ? 'It' : 'They'} will retry automatically.`,
  queuePersistence: 'Space tabs are saved on this device, but the pending sync queue could not be saved between browser sessions.',
  postRefused: (_rows, detail) => `Space tabs are saved on this device, but the server refused to sync them: ${detail}`,
  cursorPersistence: 'Space tabs are saved on this device, but the sync cursor could not be saved between browser sessions.',
}

type SpaceMembersSyncOptions = Omit<StateSyncOptions, 'namespace' | 'compare' | 'messages'>

export function createSpaceMembersSync(options: SpaceMembersSyncOptions) {
  return createStateSync({
    ...options,
    namespace: spaceMembersNamespace,
    compare: (left, right) => compareStateVersions(left.updated, left.writeID, right.updated, right.writeID),
    messages: spaceMembersSyncMessages,
  })
}

export function createSpaceMembersSyncPersistence(storage: Pick<Storage, 'getItem' | 'setItem'>): StateSyncPersistence {
  return createStateSyncPersistence(storage, spaceMembersNamespace)
}

export function spaceMembersStoreSyncAdapter(store: SpaceMembersStore): StateSyncStore {
  return {
    all: store.rows,
    merge: store.merge,
    liveIDs: () => store.rows().flatMap((row) => row.deleted ? [] : [row.key]),
    subscribeMutations: store.subscribeMutations,
  }
}

export function browserSpaceMembersTransport(): StateTransport {
  return {
    since: async (rev) => {
      try { return await getState(spaceMembersNamespace, rev) } catch (error) {
        if (error instanceof Error && 'response' in error && (error.response as Response | undefined)?.status === 404) return { rows: [], rev: 0 }
        throw error
      }
    },
    upsert: (rows) => upsertState(spaceMembersNamespace, rows),
  }
}
