import assert from 'node:assert/strict'
import test from 'node:test'

import {
  closeSpaceLayout,
  clearAllLayoutFamilies,
  initializeSpaces,
  layoutRecoveryKey,
  mainSpaceID,
  migrationMarkerKey,
  reopenSpaceLayout,
  selectActiveSpace,
  shellStorageKey,
  spaceLayoutBackupKey,
  spaceLayoutKey,
  spaceRecordKey,
} from '../src/features/spaces/spacesModel.ts'
import { persistLayoutSnapshot } from '../src/features/layout/layoutPersistenceModel.ts'
import { layoutStorageKey, type StoredLayout } from '../src/features/layout/dockLayout.ts'

class FakeStorage implements Storage {
  readonly values = new Map<string, string>()
  readonly calls: string[] = []
  failKey = ''

  get length() { return this.values.size }
  clear() { this.values.clear() }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  getItem(key: string) { this.calls.push(`get:${key}`); return this.values.get(key) ?? null }
  setItem(key: string, value: string) {
    this.calls.push(`set:${key}`)
    if (key === this.failKey) throw new Error('quota')
    this.values.set(key, value)
  }
  removeItem(key: string) { this.calls.push(`remove:${key}`); this.values.delete(key) }
}

const v3: StoredLayout = {
  version: 3,
  dock: null,
  rails: {
    fleet: { width: 315, collapsed: true },
    notes: { width: 240, collapsed: false },
  },
  expandedItems: ['workspace:w1'],
  knownWorkspaceItems: ['workspace:w1'],
}

test('v3 migrates atomically into deterministic main, dock-only v4, and global shell families', () => {
  const storage = new FakeStorage()
  storage.values.set(layoutStorageKey, JSON.stringify(v3))

  const result = initializeSpaces(storage)

  assert.equal(result.mode, 'spaces')
  assert.equal(result.activeSpaceID, mainSpaceID)
  assert.deepEqual(JSON.parse(storage.values.get(spaceRecordKey(mainSpaceID)) ?? ''), {
    version: 1,
    writeID: 'migration-v1',
    record: { id: 'main', name: 'main', order: 0, created: 0, updated: 0 },
  })
  assert.deepEqual(JSON.parse(storage.values.get(spaceLayoutKey(mainSpaceID)) ?? ''), {
    version: 4,
    dock: null,
  })
  assert.deepEqual(JSON.parse(storage.values.get(shellStorageKey) ?? ''), {
    version: 1,
    rails: v3.rails,
    expandedItems: v3.expandedItems,
    knownWorkspaceItems: v3.knownWorkspaceItems,
  })
  assert.ok(storage.values.has(migrationMarkerKey))
  assert.ok(storage.values.has(layoutStorageKey), 'legacy source remains for rollback')

  const writes = storage.calls.length
  assert.equal(initializeSpaces(storage).mode, 'spaces')
  assert.equal(storage.calls.slice(writes).some((call) => call.startsWith('set:')), false)
})

test('a failed migration keeps the complete v3 writer active and retries next load', () => {
  const storage = new FakeStorage()
  storage.values.set(layoutStorageKey, JSON.stringify(v3))
  storage.failKey = shellStorageKey

  const failed = initializeSpaces(storage)
  assert.equal(failed.mode, 'legacy')
  assert.equal(storage.values.has(migrationMarkerKey), false)

  const changed: StoredLayout = { ...v3, rails: { ...v3.rails, fleet: { width: 333, collapsed: false } } }
  const wrote = persistLayoutSnapshot(storage, failed, JSON.stringify(changed), { recovering: false, lastGoodRaw: null })
  assert.equal(wrote.wrote, true)
  assert.deepEqual(JSON.parse(storage.values.get(layoutStorageKey) ?? '').rails.fleet, { width: 333, collapsed: false })

  storage.failKey = ''
  assert.equal(initializeSpaces(storage).mode, 'spaces')
  assert.deepEqual(JSON.parse(storage.values.get(shellStorageKey) ?? '').rails.fleet, { width: 333, collapsed: false })
})

test('two first-load migrations converge on one well-known main definition', () => {
  const storage = new FakeStorage()
  storage.values.set(layoutStorageKey, JSON.stringify(v3))
  assert.equal(initializeSpaces(storage).activeSpaceID, 'main')
  storage.values.delete(migrationMarkerKey)
  assert.equal(initializeSpaces(storage).activeSpaceID, 'main')
  assert.equal([...storage.values.keys()].filter((key) => key === spaceRecordKey(mainSpaceID)).length, 1)
})

test('close verifies recovery before removing live layout and reopen restores both copies', () => {
  const storage = new FakeStorage()
  const primary = JSON.stringify({ version: 4, dock: null })
  const backup = JSON.stringify({ version: 4, dock: null, marker: 'backup' })
  storage.values.set(spaceLayoutKey('alpha'), primary)
  storage.values.set(spaceLayoutBackupKey('alpha'), backup)

  assert.equal(closeSpaceLayout(storage, 'alpha', 1_000).ok, true)
  const recoverySet = storage.calls.indexOf(`set:${layoutRecoveryKey('alpha')}`)
  const recoveryRead = storage.calls.indexOf(`get:${layoutRecoveryKey('alpha')}`, recoverySet + 1)
  const primaryRemove = storage.calls.indexOf(`remove:${spaceLayoutKey('alpha')}`)
  assert.ok(recoverySet >= 0 && recoveryRead > recoverySet && primaryRemove > recoveryRead)
  assert.equal(storage.values.has(spaceLayoutKey('alpha')), false)

  assert.equal(reopenSpaceLayout(storage, 'alpha').ok, true)
  assert.equal(storage.values.get(spaceLayoutKey('alpha')), primary)
  assert.equal(storage.values.get(spaceLayoutBackupKey('alpha')), backup)
})

test('a recovery write failure refuses close and leaves the live layout untouched', () => {
  const storage = new FakeStorage()
  storage.values.set(spaceLayoutKey('alpha'), JSON.stringify({ version: 4, dock: null }))
  storage.failKey = layoutRecoveryKey('alpha')

  const result = closeSpaceLayout(storage, 'alpha', 1_000)
  assert.equal(result.ok, false)
  assert.ok(storage.values.has(spaceLayoutKey('alpha')))
})

test('active selection prefers this tab, then global last-active, then the first live space', () => {
  const spaces = [
    { id: 'main', name: 'main', order: 0, created: 0, updated: 0 },
    { id: 'review', name: 'review', order: 1, created: 1, updated: 1 },
    { id: 'closed', name: 'closed', order: 2, created: 2, updated: 3, deleted: true as const },
  ]
  assert.equal(selectActiveSpace(spaces, 'review', 'main'), 'review')
  assert.equal(selectActiveSpace(spaces, 'missing', 'review'), 'review')
  assert.equal(selectActiveSpace(spaces, 'closed', 'missing'), 'main')
})

test('reset clears every new family and all retained legacy keys', () => {
  const storage = new FakeStorage()
  for (const key of [
    layoutStorageKey, 'herder.web.layout.v3.last-good', 'herder.web.layout.v2', 'herder.web.layout.v1',
    spaceRecordKey('main'), spaceLayoutKey('main'), spaceLayoutBackupKey('main'), layoutRecoveryKey('main'),
    shellStorageKey, 'herder.web.shell.v1.last-good', migrationMarkerKey, 'herder.web.spaces.last-active.v1',
  ]) storage.values.set(key, 'value')
  storage.values.set('herder.web.theme.v1', 'dark')

  clearAllLayoutFamilies(storage)
  assert.deepEqual([...storage.values], [['herder.web.theme.v1', 'dark']])
})
