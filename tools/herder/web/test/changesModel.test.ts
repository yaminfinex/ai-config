import assert from 'node:assert/strict'
import test from 'node:test'
import { branchBaseAvailable, changesPanelID, entryChangeCount } from '../src/features/git/changesModel.ts'

test('changes panels are stable per opaque root', () => {
  assert.equal(changesPanelID('/repo with space'), 'changes:%2Frepo%20with%20space')
})

test('base choices include branch only when its proof is available', () => {
  assert.equal(branchBaseAvailable({ status: 'unavailable', reason: 'no origin/HEAD' }), false)
  assert.equal(branchBaseAvailable({ status: 'available', default_ref: 'origin/main', default_sha: 'a', merge_base: 'b' }), true)
})

test('entry counts stay absent when numstat could not prove them', () => {
  assert.equal(entryChangeCount({ additions: 7, deletions: 3 }), '+7 / −3')
  assert.equal(entryChangeCount({}), '')
})
