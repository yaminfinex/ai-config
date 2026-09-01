import assert from 'node:assert/strict'
import test from 'node:test'

import { queueComposerNote } from '../src/features/notes/noteQueue.ts'

function store({ persistent = true, failFlush = false } = {}) {
  const notes = new Map<string, { id: string }>()
  let problem = persistent ? '' : 'Browser storage is unavailable.'
  return {
    notes,
    add: ({ text }: { group: string, text: string }) => {
      const note = { id: 'queued', group: 'kilo', text, created: 1, updated: 1 }
      notes.set(note.id, note)
      return { ok: true as const, value: note }
    },
    delete: (ids: string[]) => { ids.forEach((id) => notes.delete(id)); return { ok: true as const, value: ids.length } },
    flush: () => {
      if (failFlush) { persistent = false; problem = 'Quota exceeded.'; return false }
      return true
    },
    status: () => ({ persistent, recovered: false, problem }),
  }
}

test('composer queue proves persistence before allowing the draft to clear', () => {
  const healthy = store()
  assert.deepEqual(queueComposerNote(healthy, 'kilo', 'keep this'), { ok: true })
  assert.equal(healthy.notes.size, 1)

  const blocked = store({ persistent: false })
  assert.deepEqual(queueComposerNote(blocked, 'kilo', 'keep this'), { ok: false, reason: 'Browser storage is unavailable. The composer draft was left unchanged.' })
  assert.equal(blocked.notes.size, 0)

  const failed = store({ failFlush: true })
  assert.deepEqual(queueComposerNote(failed, 'kilo', 'keep this'), { ok: false, reason: 'Quota exceeded. The composer draft was left unchanged.' })
  assert.equal(failed.notes.size, 0)
})
