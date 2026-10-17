import assert from 'node:assert/strict'
import test from 'node:test'

import {
  laneMoveKey, laneOfSpace, laneRoster, laneSpaceID, laneSpaces, moveStoredLayout, pendingLaneMoves, readLaneMovesApplied,
  rekeyRecord, runLaneMoves, withLaneRosters, writeLaneMovesApplied, type LaneMoveTarget,
} from '../src/features/spaces/laneSpacesModel.ts'
import { spaceLayoutBackupKey, spaceLayoutKey } from '../src/features/spaces/spacesModel.ts'
import type { Board } from '../src/types.ts'

const board: Board = {
  workspaces: [], unplaced: [],
  lanes: [
    { id: 'aa11', name: 'payload', lead: 'impl-geni', members: ['impl-hine', 'impl-geni'] },
    { id: 'bb22', name: 'audit', lead: '', members: ['impl-bomi'] },
  ],
}

function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial))
  return {
    data,
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => { data.set(key, value) },
    removeItem: (key: string) => { data.delete(key) },
  }
}

test('every board lane is a space lane:<id> with its name, ordered ahead of the user\'s spaces', () => {
  assert.deepEqual(laneSpaces(board), [
    { id: 'lane:aa11', name: 'payload', order: -2, created: 0, updated: 0 },
    { id: 'lane:bb22', name: 'audit', order: -1, created: 0, updated: 0 },
  ])
  assert.deepEqual(laneSpaces(undefined), [])
  assert.deepEqual(laneSpaces({ workspaces: [], unplaced: [] }), [], 'a lane with no live member is not on the board, so no space')
  assert.equal(laneSpaceID('aa11'), 'lane:aa11')
  assert.equal(laneOfSpace('lane:aa11'), 'aa11')
  assert.equal(laneOfSpace('s-123'), null)
  assert.equal(laneOfSpace(null), null)
})

test('a lane space\'s members are its roster, lead first, then its pins without repeats', () => {
  assert.deepEqual(laneRoster(board.lanes![0]), ['impl-geni', 'impl-hine'])
  assert.deepEqual(laneRoster(board.lanes![1]), ['impl-bomi'], 'no lead keeps the board order')
  const pinned = { 'lane:aa11': [{ kind: 'agent' as const, name: 'impl-hine' }, { kind: 'agent' as const, name: 'ziru' }], own: [{ kind: 'agent' as const, name: 'x' }] }
  const merged = withLaneRosters(pinned, board.lanes)
  assert.deepEqual(merged['lane:aa11'].map((member) => member.kind === 'agent' ? member.name : ''), ['impl-geni', 'impl-hine', 'ziru'])
  assert.deepEqual(merged['lane:bb22'], [{ kind: 'agent', name: 'impl-bomi' }])
  assert.equal(merged.own, pinned.own)
  assert.equal(withLaneRosters(pinned, undefined), pinned, 'no lanes: the same record')
})

function recordingTarget(live: string[]) {
  const calls: string[] = []
  const target: LaneMoveTarget = {
    hasSpace: (id) => live.includes(id),
    moveMembers: (from, to) => calls.push(`members ${from}->${to}`),
    closeSpace: (id) => calls.push(`close ${id}`),
    moveLayout: (from, to) => calls.push(`layout ${from}->${to}`),
    moveTier: (from, to) => calls.push(`tier ${from}->${to}`),
  }
  return { calls, target }
}

test('the lane move applies each pair once: members and the tombstone only while the old row is live, layout and tier always', () => {
  const moves = { 's-pay': 'aa11', 's-gone': 'bb22' }
  const { calls, target } = recordingTarget(['s-pay'])
  const applied = runLaneMoves(moves, [], target)
  assert.deepEqual(applied, ['s-pay', 's-gone'])
  assert.deepEqual(calls, [
    'members s-pay->lane:aa11', 'close s-pay', 'layout s-pay->lane:aa11', 'tier s-pay->lane:aa11',
    'layout s-gone->lane:bb22', 'tier s-gone->lane:bb22',
  ])
  const again = recordingTarget(['s-pay'])
  assert.deepEqual(runLaneMoves(moves, applied, again.target), applied)
  assert.deepEqual(again.calls, [], 'a second run does nothing')
  assert.deepEqual(pendingLaneMoves({ ...moves, 's-new': 'cc33', '': 'x', 's-empty': '' }, applied), [['s-new', 'cc33']])
})

test('the applied list round-trips through its versioned record and starts empty', () => {
  const storage = memoryStorage()
  assert.deepEqual(readLaneMovesApplied(storage).applied, [])
  writeLaneMovesApplied(storage, ['s-pay'])
  assert.deepEqual(JSON.parse(storage.data.get(laneMoveKey)!), { version: 1, applied: ['s-pay'] })
  assert.deepEqual(readLaneMovesApplied(storage).applied, ['s-pay'])
})

test('a stored layout moves with its backup only into an empty key; a record re-keys only into an empty key', () => {
  const storage = memoryStorage({ [spaceLayoutKey('s-pay')]: 'L', [spaceLayoutBackupKey('s-pay')]: 'B' })
  moveStoredLayout(storage, 's-pay', 'lane:aa11')
  assert.equal(storage.getItem(spaceLayoutKey('lane:aa11')), 'L')
  assert.equal(storage.getItem(spaceLayoutBackupKey('lane:aa11')), 'B')
  assert.equal(storage.getItem(spaceLayoutKey('s-pay')), null)
  const taken = memoryStorage({ [spaceLayoutKey('s-pay')]: 'L', [spaceLayoutKey('lane:aa11')]: 'mine' })
  moveStoredLayout(taken, 's-pay', 'lane:aa11')
  assert.equal(taken.getItem(spaceLayoutKey('lane:aa11')), 'mine')
  assert.equal(taken.getItem(spaceLayoutKey('s-pay')), 'L', 'nothing is lost when the target is taken')
  assert.deepEqual(rekeyRecord({ 's-pay': 2, b: 1 }, 's-pay', 'lane:aa11'), { b: 1, 'lane:aa11': 2 })
  assert.deepEqual(rekeyRecord({ 's-pay': 2, 'lane:aa11': 1 }, 's-pay', 'lane:aa11'), { 'lane:aa11': 1 })
  const untouched = { b: 1 }
  assert.equal(rekeyRecord(untouched, 's-pay', 'lane:aa11'), untouched)
})
