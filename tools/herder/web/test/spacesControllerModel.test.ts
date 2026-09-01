import assert from 'node:assert/strict'
import test from 'node:test'

import {
  moveBeforeActiveClose,
  performSpaceSwitch,
  sendPanelToExistingSpace,
  sendPanelToNewSpace,
  createAndSwitchSpace,
  spaceIDInDirection,
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

test('space cycling follows stored order and wraps both directions', () => {
  const spaces = [
    { id: 'a', name: 'a', order: 0, created: 0, updated: 0 },
    { id: 'b', name: 'b', order: 1, created: 0, updated: 0 },
    { id: 'c', name: 'c', order: 2, created: 0, updated: 0 },
  ]
  assert.equal(spaceIDInDirection(spaces, 'c', 'next'), 'a')
  assert.equal(spaceIDInDirection(spaces, 'a', 'previous'), 'c')
  assert.equal(spaceIDInDirection(spaces, 'missing', 'next'), 'a')
})

test('panel transfer closes the source only after the target write succeeds', () => {
  const calls: string[] = []
  const failed = sendPanelToExistingSpace('target', { id: 'panel' }, {
    write: () => { calls.push('write'); return { ok: false, reason: 'corrupt' } },
    closeSource: () => { calls.push('close') },
  })
  assert.equal(failed.ok, false)
  assert.deepEqual(calls, ['write'])

  calls.length = 0
  const sent = sendPanelToExistingSpace('target', { id: 'panel' }, {
    write: () => { calls.push('write'); return { ok: true, duplicate: false } },
    closeSource: () => { calls.push('close') },
  })
  assert.equal(sent.ok, true)
  assert.deepEqual(calls, ['write', 'close'])
})

test('send-to-new uses create and rolls back when the layout mutation fails', () => {
  const fresh = { id: 'fresh', name: 'space 2', order: 1, created: 1, updated: 1 }
  const calls: string[] = []
  const result = sendPanelToNewSpace({ id: 'panel' }, {
    create: () => { calls.push('create'); return { ok: true, value: fresh } },
    rollbackCreate: (id) => { calls.push(`rollback:${id}`); return true },
    write: (id) => { calls.push(`write:${id}`); return { ok: false, reason: 'write' } },
    flush: () => { calls.push('flush'); return true },
    closeSource: () => { calls.push('close') },
  })
  assert.equal(result.ok, false)
  assert.deepEqual(calls, ['create', 'write:fresh', 'rollback:fresh'])
})

test('send-to-new flushes the shared store before closing the source', () => {
  const fresh = { id: 'fresh', name: 'space 2', order: 1, created: 1, updated: 1 }
  const calls: string[] = []
  assert.equal(sendPanelToNewSpace({ id: 'panel' }, {
    create: () => { calls.push('create'); return { ok: true, value: fresh } },
    rollbackCreate: () => false,
    write: (id) => { calls.push(`write:${id}`); return { ok: true, duplicate: false } },
    flush: () => { calls.push('flush'); return true },
    closeSource: () => { calls.push('close') },
  }).ok, true)
  assert.deepEqual(calls, ['create', 'write:fresh', 'flush', 'close'])
})

test('palette space creation renames then switches, rolling back either failure', () => {
  const fresh = { id: 'fresh', name: 'space 2', order: 1, created: 1, updated: 1 }
  for (const failAt of ['rename', 'switch'] as const) {
    const calls: string[] = []
    const result = createAndSwitchSpace('review', {
      create: () => { calls.push('create'); return { ok: true, value: fresh } },
      rename: () => { calls.push('rename'); return failAt === 'rename' ? { ok: false, reason: 'rename failed' } : { ok: true, value: { ...fresh, name: 'review' } } },
      switchTo: () => { calls.push('switch'); return failAt !== 'switch' },
      rollbackCreate: (id) => { calls.push(`rollback:${id}`); return true },
      flush: () => { calls.push('flush'); return true },
    })
    assert.equal(result.ok, false)
    assert.deepEqual(calls, failAt === 'rename'
      ? ['create', 'rename', 'rollback:fresh']
      : ['create', 'rename', 'switch', 'rollback:fresh'])
  }
})

test('palette space creation flushes only after a successful switch', () => {
  const fresh = { id: 'fresh', name: 'space 2', order: 1, created: 1, updated: 1 }
  const calls: string[] = []
  assert.equal(createAndSwitchSpace('review', {
    create: () => { calls.push('create'); return { ok: true, value: fresh } },
    rename: () => { calls.push('rename'); return { ok: true, value: { ...fresh, name: 'review' } } },
    switchTo: () => { calls.push('switch'); return true },
    rollbackCreate: () => false,
    flush: () => { calls.push('flush'); return true },
  }).ok, true)
  assert.deepEqual(calls, ['create', 'rename', 'switch', 'flush'])
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
