import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

import { addPendingGroup, maxGroupNameLength, realGroupLabels, removePendingGroup, settledPendingGroups, validateGroupName } from '../src/features/sidebar/pendingGroupsModel.ts'
import { buildGroupNodes, ungroupedID } from '../src/features/sidebar/sidebarNodes.ts'
import type { Board, Row } from '../src/types.ts'

function agent(name: string, extra: Partial<Row> = {}): Row {
  return { pane_id: '-', agent: name, tool: 'claude', herdr_status: '-', bus_status: 'listening', gap: 'no visible pane', ...extra }
}

function board(groups: Record<string, string | undefined>): Board {
  return { workspaces: [], unplaced: Object.entries(groups).map(([name, group], index) => agent(name, { group, manager: 'operator', manager_state: 'operator', created_at: `2026-09-0${index + 1}T00:00:00Z` })) }
}

test('a group name follows the server rules: trimmed, non-empty, at most 80 runes, no control characters, not an existing label', () => {
  assert.deepEqual(validateGroupName('  unit-x ', []), { ok: true, name: 'unit-x' })
  assert.deepEqual(validateGroupName('   ', []), { ok: false, reason: 'group name is empty' })
  assert.deepEqual(validateGroupName('bad\tname', []), { ok: false, reason: 'group name must not contain control characters' })
  assert.equal(validateGroupName('é'.repeat(maxGroupNameLength), []).ok, true, '80 runes, not bytes')
  assert.deepEqual(validateGroupName('x'.repeat(maxGroupNameLength + 1), []), { ok: false, reason: 'group name must not exceed 80 characters' })
  assert.deepEqual(validateGroupName('audit', ['fleet-refit', 'audit']), { ok: false, reason: 'group audit already exists' })
  assert.equal(validateGroupName('Audit', ['audit']).ok, true, 'labels are case-sensitive, like the server')
})

test('pending groups add once and remove by label', () => {
  assert.deepEqual(addPendingGroup([], 'unit-x'), ['unit-x'])
  assert.deepEqual(addPendingGroup(['unit-x'], 'unit-x'), ['unit-x'])
  assert.deepEqual(addPendingGroup(['unit-x'], 'unit-y'), ['unit-x', 'unit-y'])
  assert.deepEqual(removePendingGroup(['unit-x', 'unit-y'], 'unit-x'), ['unit-y'])
})

test('a placeholder header is drawn empty after the real headers and before Ungrouped, never twice, and carries the label a drop writes', () => {
  const nodes = buildGroupNodes(board({ ziru: 'audit', lubo: undefined }), ['unit-x', 'audit', '', 'unit-x'])
  assert.deepEqual(nodes.get('tree-root')?.children, ['group:audit', 'group:unit-x', ungroupedID])
  const placeholder = nodes.get('group:unit-x')!
  assert.equal(placeholder.placeholder, true)
  assert.equal(placeholder.group, 'unit-x')
  assert.deepEqual(placeholder.children, [])
  assert.equal(placeholder.count, 0)
  assert.equal(nodes.get('group:audit')?.placeholder, undefined, 'a real header is never a placeholder')
  assert.deepEqual(realGroupLabels(nodes), ['audit'])
  // Every row under a header carries that header's label; rows under Ungrouped carry ''.
  assert.equal(nodes.get('group:audit/agent:ziru')?.group, 'audit')
  assert.equal(nodes.get('group:/agent:lubo')?.group, '')
  assert.equal(buildGroupNodes(board({ ziru: 'audit' })).get('group:audit/agent:ziru')?.group, 'audit', 'default argument: no pending groups')
})

test('a placeholder settles only once a frame shows its label with members', () => {
  const before = buildGroupNodes(board({ ziru: 'audit' }), ['unit-x'])
  assert.deepEqual(settledPendingGroups(['unit-x'], before), [])
  const after = buildGroupNodes(board({ ziru: 'unit-x' }), ['unit-x'])
  assert.deepEqual(settledPendingGroups(['unit-x'], after), ['unit-x'])
  assert.equal(after.get('group:unit-x')?.placeholder, undefined, 'the real header replaces the placeholder in the same frame')
  assert.deepEqual(settledPendingGroups(['unit-y'], after), [])
})

test('the sidebar owns the new-group row: hover-reveal button, inline input, Enter commits through the validator, Escape and blur cancel, placeholders get × and no space button', () => {
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.match(sidebar, /className="group-space-button group-create-button"/)
  assert.match(sidebar, /if \(event\.key === 'Enter'\) commitNewGroup\(\)/)
  assert.match(sidebar, /if \(event\.key === 'Escape'\) setNewGroup\(null\)/)
  assert.match(sidebar, /onBlur=\{\(\) => setNewGroup\(null\)\}/)
  assert.match(sidebar, /validateGroupName\(newGroup\.value, \[\.\.\.realGroupLabels\(groupNodes\), \.\.\.pendingGroups\]\)/, 'refused against real and pending labels')
  assert.match(sidebar, /groupHeader && !node\.placeholder && <button type="button" className="group-space-button"/)
  assert.match(sidebar, /node\.placeholder && <button type="button" className="group-space-button group-remove-button"/)
  assert.match(sidebar, /settledPendingGroups\(pendingGroups, groupNodes\)/)
  assert.match(sidebar, /buildGroupNodes\(board, pendingGroups\)/)
})
