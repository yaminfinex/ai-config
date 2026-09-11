import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

import { beginRename, editingAt, prepareRename, renameValue } from '../src/features/sidebar/renameModel.ts'

test('begin pre-fills the current title and changed commits trim their payload', () => {
  const begun = beginRename('impl-pini', 'agent:impl-pini', 'display names')
  assert.equal(begun.value, 'display names')
  assert.equal(prepareRename(renameValue(begun, '  better name  ')), 'better name')
})

test('empty and unchanged values cancel without a request', () => {
  assert.equal(prepareRename(renameValue(beginRename('impl-pini', 'agent:impl-pini', 'display names'), ' display names ')), null)
  assert.equal(prepareRename(renameValue(beginRename('impl-pini', 'agent:impl-pini', 'display names'), '  ')), null)
  assert.equal(prepareRename(renameValue(beginRename('impl-pini', 'agent:impl-pini'), '   ')), null)
})

test('one editor per initiating node: an agent under two group headers mounts the editor only where the rename started', () => {
  const state = beginRename('ziru', 'group:audit/agent:ziru', 'web-orchestrator')
  assert.equal(state.nodeID, 'group:audit/agent:ziru')
  assert.deepEqual(editingAt(state, 'group:audit/agent:ziru'), state)
  assert.equal(editingAt(state, 'group:fleet-refit/agent:ziru'), null, 'the second occurrence renders a plain label')
  assert.equal(editingAt(state, 'agent:ziru'), null)
  assert.equal(editingAt(null, 'group:audit/agent:ziru'), null)
  // The occurrences share one input ref, so exactly one may mount it: the component decides through editingAt by node id,
  // starts the rename with the clicked node's id, and re-selects the input when the initiating node changes.
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.match(sidebar, /const editing = pane \? editingAt\(renaming, node\.id\) : null/)
  assert.equal(sidebar.match(/startRename\(pane\.agent, node\.id, pane\.title\)/g)?.length, 2)
  assert.match(sidebar, /renameInput\.current\?\.select\(\) \}, \[renaming\?\.nodeID\]/)
  assert.doesNotMatch(sidebar, /renaming\?\.name === pane\.agent/)
  // The annotation is still written by agent name, never by node id.
  assert.match(sidebar, /await renameAgent\(renaming\.name, title\)/)
})
