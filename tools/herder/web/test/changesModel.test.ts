import assert from 'node:assert/strict'
import test from 'node:test'
import { branchBaseAvailable, branchChangeSummary, changeSideLabel, changesPanelID, effectiveChangesBase, entryChangeCount, requestedChangesBase } from '../src/features/git/changesModel.ts'

test('changes panels are stable per opaque root', () => {
  assert.equal(changesPanelID('/repo with space'), 'changes:%2Frepo%20with%20space')
})

test('base choices include branch only when its proof is available', () => {
  assert.equal(branchBaseAvailable({ status: 'unavailable', reason: 'no origin/HEAD' }), false)
  assert.equal(branchBaseAvailable({ status: 'available', default_ref: 'origin/main', default_sha: 'a', merge_base: 'b' }), true)
})

test('branch is the default request while explicit choices and server fallback win', () => {
  assert.equal(requestedChangesBase(null), 'branch')
  assert.equal(requestedChangesBase('uncommitted'), 'uncommitted')
  assert.equal(requestedChangesBase('branch'), 'branch')
  assert.equal(effectiveChangesBase({ kind: 'branch' }), 'branch')
  assert.equal(effectiveChangesBase({ kind: 'uncommitted' }), 'uncommitted')
  assert.equal(effectiveChangesBase(undefined), 'uncommitted')
})

test('entry counts stay absent when numstat could not prove them', () => {
  assert.equal(entryChangeCount({ additions: 7, deletions: 3 }), '+7 / −3')
  assert.equal(entryChangeCount({}), '')
})

test('branch-only rows are presented as committed without overloading porcelain state', () => {
  assert.equal(changeSideLabel({ kind: 'renamed', staged: false, unstaged: false }, 'branch'), 'committed')
  assert.equal(changeSideLabel({ kind: 'modified', staged: true, unstaged: true }, 'branch'), 'staged + unstaged')
  assert.equal(changeSideLabel({ kind: 'untracked', staged: false, unstaged: true }, 'branch'), 'unstaged')
  assert.equal(changeSideLabel({ kind: 'modified', staged: false, unstaged: false }, 'uncommitted'), '')
})

test('branch summaries describe changed files without calling them uncommitted', () => {
  assert.equal(branchChangeSummary(5, 27), '5 commits ahead; 27 changed files vs merge-base')
  assert.equal(branchChangeSummary(1, 1), '1 commit ahead; 1 changed file vs merge-base')
  assert.equal(branchChangeSummary(undefined, 0), '0 changed files vs merge-base')
})
