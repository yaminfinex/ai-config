import assert from 'node:assert/strict'
import test from 'node:test'

import {
  moveBeforeActiveClose,
  performSpaceSwitch,
  type SpaceDockSource,
} from '../src/features/spaces/spacesControllerModel.ts'

type Dock = { name: string }

function source(primary: Dock | null, backup: Dock | null, problem = false): SpaceDockSource<Dock> {
  return {
    stored: primary === null ? null : { dock: primary },
    backup: backup === null ? null : { dock: backup },
    problem,
  }
}

test('space switch flushes, suspends, restores through backup, completes, then stamps history', () => {
  const calls: string[] = []
  const result = performSpaceSwitch('review', {
    flush: () => { calls.push('flush'); return true },
    suspend: () => { calls.push('suspend') },
    read: (id) => { calls.push(`read:${id}`); return source({ name: 'primary' }, { name: 'backup' }) },
    withHistorySuppressed: (operation) => { calls.push('history:suppress'); return operation() },
    dock: { clear: () => { calls.push('dock:clear') } },
    restore: (_dock, layout) => { calls.push(`restore:${layout.name}`); return layout.name === 'backup' },
    recoveredFromBackup: (id) => { calls.push(`recovered:${id}`) },
    complete: (id) => { calls.push(`complete:${id}`) },
    persistActive: (id) => { calls.push(`active:${id}`); return true },
    replaceStamp: () => { calls.push('history:replace') },
    finish: ({ restoreFailed }) => { calls.push(`finish:${restoreFailed}`) },
  })

  assert.equal(result, true)
  assert.deepEqual(calls, [
    'flush', 'suspend', 'read:review', 'history:suppress',
    'dock:clear', 'restore:primary', 'dock:clear', 'restore:backup', 'recovered:review',
    'complete:review', 'active:review', 'history:replace', 'finish:true',
  ])
})

test('failed primary and backup restore clears to empty before the visible failure result', () => {
  const calls: string[] = []
  performSpaceSwitch('review', {
    flush: () => true,
    suspend: () => { calls.push('suspend') },
    read: () => source({ name: 'primary' }, { name: 'backup' }),
    withHistorySuppressed: (operation) => operation(),
    dock: { clear: () => { calls.push('dock:clear') } },
    restore: (_dock, layout) => { calls.push(`restore:${layout.name}`); return false },
    recoveredFromBackup: () => { calls.push('recovered') },
    complete: () => { calls.push('complete') },
    persistActive: () => true,
    replaceStamp: () => { calls.push('history:replace') },
    finish: ({ restoreFailed }) => { calls.push(restoreFailed ? 'banner' : 'quiet') },
  })
  assert.deepEqual(calls, [
    'suspend', 'dock:clear', 'restore:primary', 'dock:clear', 'restore:backup',
    'dock:clear', 'complete', 'history:replace', 'banner',
  ])
})

test('active close chooses the next neighbor, then the previous neighbor', () => {
  const spaces = [
    { id: 'a', name: 'a', order: 0, created: 0, updated: 0 },
    { id: 'b', name: 'b', order: 1, created: 0, updated: 0 },
    { id: 'c', name: 'c', order: 2, created: 0, updated: 0 },
  ]
  const switched: string[] = []
  const dependencies = {
    create: () => { throw new Error('not needed') },
    rollbackCreate: () => { throw new Error('not needed') },
    switchTo: (id: string) => { switched.push(id); return true },
  }
  assert.equal(moveBeforeActiveClose('b', 'b', spaces, dependencies).ok, true)
  assert.equal(moveBeforeActiveClose('c', 'c', spaces, dependencies).ok, true)
  assert.deepEqual(switched, ['c', 'b'])
})

test('last-space close rolls back a fresh replacement when switching fails', () => {
  const only = { id: 'main', name: 'main', order: 0, created: 0, updated: 0 }
  const replacement = { id: 'fresh', name: 'space 2', order: 1, created: 1, updated: 1 }
  const calls: string[] = []
  const result = moveBeforeActiveClose('main', 'main', [only], {
    create: () => { calls.push('create'); return { ok: true, value: replacement } },
    rollbackCreate: (id) => { calls.push(`rollback:${id}`); return true },
    switchTo: (id) => { calls.push(`switch:${id}`); return false },
  })
  assert.equal(result.ok, false)
  assert.deepEqual(calls, ['create', 'switch:fresh', 'rollback:fresh'])
})

test('last-space close also rolls back when switching throws mid-operation', () => {
  const only = { id: 'main', name: 'main', order: 0, created: 0, updated: 0 }
  const replacement = { id: 'fresh', name: 'space 2', order: 1, created: 1, updated: 1 }
  const rolledBack: string[] = []
  const result = moveBeforeActiveClose('main', 'main', [only], {
    create: () => ({ ok: true, value: replacement }),
    rollbackCreate: (id) => { rolledBack.push(id); return true },
    switchTo: () => { throw new Error('dock restore exploded') },
  })
  assert.equal(result.ok, false)
  assert.deepEqual(rolledBack, ['fresh'])
})
