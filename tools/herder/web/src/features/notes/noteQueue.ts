import type { NotesStore } from './notesStore.ts'

type QueueStore = Pick<NotesStore, 'add' | 'delete' | 'flush' | 'status'>

function refusal(problem: string) {
  return { ok: false as const, reason: `${problem || 'This note could not be saved durably.'} The composer draft was left unchanged.` }
}

export function queueComposerNote(store: QueueStore, group: string, text: string) {
  const before = store.status()
  if (!before.persistent) return refusal(before.problem)
  const added = store.add({ group, text })
  if (!added.ok) return added
  store.flush()
  const after = store.status()
  if (!after.persistent) {
    store.delete([added.value.id])
    return refusal(after.problem)
  }
  return { ok: true as const }
}
