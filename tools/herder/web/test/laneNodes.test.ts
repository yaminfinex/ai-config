import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { buildLaneNodes, buildSupervisionNodes, collapsedLabel, laneHeaderID, noLaneID, pendingLaneID, treeSummary } from '../src/features/sidebar/sidebarNodes.ts'
import { laneHeaderTooltip, planLaneDrop, validateLaneName } from '../src/features/sidebar/laneDropModel.ts'
import { managerItems, reconcileExpansion } from '../src/features/sidebar/sidebarView.ts'
import type { Board, Lane, Row } from '../src/types.ts'

function agent(name: string, extra: Partial<Row> & { pane_id?: string }): Row {
  return { pane_id: extra.pane_id ?? '-', agent: name, tool: 'claude', herdr_status: extra.pane_id ? 'unknown' : '-', bus_status: 'listening', gap: extra.pane_id ? '-' : 'no visible pane', ...extra }
}

// ziru manages hine and geni (lane refit, lead geni), bomi (lane audit) and
// doza (no lane); ziru itself is in no lane. vara manages pono (audit, the
// lead). tume has a Task subagent; tume is in no lane. One terminal pane
// must not appear anywhere.
const refit: Lane = { id: 'aaaa1111', name: 'fleet-refit', lead: 'impl-geni', members: ['impl-hine', 'impl-geni'] }
const audit: Lane = { id: 'bbbb2222', name: 'audit', lead: 'impl-pono', members: ['impl-bomi', 'impl-pono'] }
function board(lanes: Lane[] = [refit, audit]): Board {
  return {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'fleet', focused: false, pane_count: 6, tab_count: 1, active_tab_id: 't1', agent_status: 'active',
      tabs: [{ tab_id: 't1', number: 1, label: 'seats', focused: false, pane_count: 6, agent_status: 'active', panes: [
        agent('ziru', { pane_id: 'w1:p1', manager: 'operator', manager_state: 'operator', created_at: '2026-09-01T00:00:00Z' }),
        agent('impl-hine', { pane_id: 'w1:p2', manager: 'ziru', manager_state: 'live', bus_status: 'active', created_at: '2026-09-02T00:00:00Z' }),
        agent('impl-bomi', { pane_id: 'w1:p3', manager: 'ziru', manager_state: 'live', created_at: '2026-09-03T00:00:00Z' }),
        agent('vara', { pane_id: 'w1:p4', manager: 'operator', manager_state: 'operator', created_at: '2026-09-01T01:00:00Z' }),
        agent('grill-tume', { pane_id: 'w1:p5', manager: 'operator', manager_state: 'operator', created_at: '2026-09-04T00:00:00Z', title: 'grill',
          subagents: [agent('tume_general_purpose_1', { parent_agent: 'grill-tume', bus_status: 'active', created_at: '2026-09-05T00:00:00Z' })] }),
        { pane_id: 'w1:p6', label: 'lima', agent: '-', tool: '-', herdr_status: '-', bus_status: '-', gap: '-' },
      ] }],
    }],
    unplaced: [
      agent('impl-geni', { manager: 'ziru', manager_state: 'live', created_at: '2026-09-02T01:00:00Z' }),
      agent('design-doza', { manager: 'ziru', manager_state: 'live', created_at: '2026-09-02T02:00:00Z' }),
      agent('impl-pono', { manager: 'vara', manager_state: 'live', created_at: '2026-09-02T03:00:00Z' }),
    ],
    lanes,
  }
}

const refitID = laneHeaderID(refit.id)
const auditID = laneHeaderID(audit.id)

test('lanes view: one header per board lane in board order, lead marked and first, No lane last', () => {
  const nodes = buildLaneNodes(board())
  assert.deepEqual(nodes.get('tree-root')?.children, [refitID, auditID, noLaneID])
  const header = nodes.get(refitID)!
  assert.equal(header.kind, 'lane')
  assert.equal(header.name, 'fleet-refit')
  assert.equal(header.lane, refit.id)
  assert.equal(header.secondary, 'lead impl-geni')
  assert.deepEqual(header.children, [`${refitID}/agent:impl-geni`, `${refitID}/agent:impl-hine`], 'the lead comes first though hine is older')
  assert.equal(nodes.get(`${refitID}/agent:impl-geni`)?.lead, true)
  assert.equal(nodes.get(`${refitID}/agent:impl-hine`)?.lead, undefined)
  assert.equal(nodes.get(`${refitID}/agent:impl-hine`)?.lane, refit.id, 'every row under a lane carries the lane id the drop uses')
  assert.deepEqual(nodes.get(auditID)?.children, [`${auditID}/agent:impl-pono`, `${auditID}/agent:impl-bomi`])
  // A manager in no lane does not pull its reports out of their lanes.
  assert.deepEqual(nodes.get(noLaneID)?.children, [`${noLaneID}/agent:ziru`, `${noLaneID}/agent:vara`, `${noLaneID}/agent:grill-tume`])
  assert.deepEqual(nodes.get(`${noLaneID}/agent:ziru`)?.children, [`${noLaneID}/agent:design-doza`])
  assert.equal(nodes.get(noLaneID)?.lane, undefined)
  assert.equal([...nodes.values()].some((node) => node.pane?.agent === '-'), false, 'terminals are not drawn')
})

test('a Task subagent appears once, beneath its owner in the owner\'s lane', () => {
  const nodes = buildLaneNodes(board())
  assert.deepEqual(nodes.get(`${noLaneID}/agent:grill-tume`)?.children, [`${noLaneID}/agent:tume_general_purpose_1`])
  assert.equal([...nodes.values()].filter((node) => node.pane?.agent === 'tume_general_purpose_1').length, 1)
})

test('a board with no lanes draws only No lane; a fully laned board draws no No lane header', () => {
  assert.deepEqual(buildLaneNodes(board([])).get('tree-root')?.children, [noLaneID])
  const owner = agent('owner', { manager_state: 'operator' })
  const laned = buildLaneNodes({ workspaces: [], unplaced: [owner], lanes: [{ id: 'c3', name: 'solo', lead: 'owner', members: ['owner'] }] })
  assert.deepEqual(laned.get('tree-root')?.children, [laneHeaderID('c3')])
  assert.equal(buildLaneNodes(undefined).get('tree-root')?.children.length, 0)
})

test('header ids are the lane id, so a rename keeps expansion and a new lane opens once like a new manager', () => {
  const before = buildLaneNodes(board())
  const renamed = buildLaneNodes(board([{ ...refit, name: 'payload' }, audit]))
  assert.equal(renamed.get(refitID)?.name, 'payload')
  assert.deepEqual([...renamed.keys()].sort(), [...before.keys()].sort())
  for (const id of [refitID, auditID, noLaneID]) assert.ok(managerItems(before).includes(id), id)
  const supervision = buildSupervisionNodes(board())
  const state = { expandedItems: [refitID], knownWorkspaceItems: [], knownManagerItems: [...managerItems(supervision), ...managerItems(before)] }
  assert.equal(reconcileExpansion(new Map(), supervision, state, renamed), null, 'a rename opens nothing')
  const grown = buildLaneNodes(board([refit, audit, { id: 'dddd4444', name: 'docs', lead: 'design-doza', members: ['design-doza'] }]))
  const added = reconcileExpansion(new Map(), supervision, state, grown)
  assert.deepEqual(added?.expandedItems, [refitID, laneHeaderID('dddd4444')])
})

test('"+ lane" adds one empty placeholder header ahead of No lane; it reads its zero count', () => {
  const nodes = buildLaneNodes(board(), 'docs')
  assert.deepEqual(nodes.get('tree-root')?.children, [refitID, auditID, pendingLaneID, noLaneID])
  const pending = nodes.get(pendingLaneID)!
  assert.equal(pending.placeholder, true)
  assert.equal(pending.lane, undefined)
  assert.equal(collapsedLabel(pending), 'docs (0)')
  assert.match(laneHeaderTooltip(pending, 0), /drop an agent here to create it with that agent as lead/)
})

test('a drop on a lane header or any row under it moves the seat; on the placeholder it creates the lane with that lead', () => {
  const nodes = buildLaneNodes(board(), 'docs')
  assert.deepEqual(planLaneDrop('lanes', 'ziru', nodes.get(refitID)), { name: 'ziru', lane: refit.id })
  assert.deepEqual(planLaneDrop('lanes', 'ziru', nodes.get(`${auditID}/agent:impl-bomi`)), { name: 'ziru', lane: audit.id })
  assert.deepEqual(planLaneDrop('lanes', 'ziru', nodes.get(pendingLaneID)), { name: 'ziru', newLane: 'docs' })
  assert.equal(planLaneDrop('lanes', 'ziru', nodes.get(noLaneID)), null, 'No lane is not a lane')
  assert.equal(planLaneDrop('lanes', 'impl-bomi', nodes.get(`${auditID}/agent:impl-bomi`)), null, 'its own row')
  assert.equal(planLaneDrop('supervision', 'ziru', nodes.get(refitID)), null)
  assert.equal(planLaneDrop('lanes', '-', nodes.get(refitID)), null)
})

test('a new lane name follows the serve: trimmed, not empty, no leading dash, no control characters, at most 80, not taken', () => {
  assert.deepEqual(validateLaneName('  docs ', ['audit']), { ok: true, name: 'docs' })
  assert.equal(validateLaneName('  ', []).ok, false)
  assert.equal(validateLaneName('-x', []).ok, false)
  assert.equal(validateLaneName('a\tb', []).ok, false)
  assert.equal(validateLaneName('x'.repeat(81), []).ok, false)
  assert.deepEqual(validateLaneName('AUDIT', ['audit']), { ok: false, reason: 'lane audit already exists' })
})

test('every lane header reads "<name> (N)" in either state, while folded supervision rows keep their summaries', () => {
  const nodes = buildLaneNodes(board())
  for (const id of [refitID, auditID, noLaneID]) {
    const node = nodes.get(id)!
    const text = `${node.name} ${treeSummary(node, false)}`
    assert.equal(text, `${node.name} ${treeSummary(node, true)}`, id)
    assert.match(text, /^\S.* \(\d+\)$/, id)
  }
  assert.equal(treeSummary(nodes.get(`${noLaneID}/agent:ziru`)!, false), '')
  assert.equal(collapsedLabel(buildSupervisionNodes(board()).get('agent:ziru')!), 'ziru (4)')
})

test('the sidebar wires the lanes view: one drop seam, "+ lane", make lead, the lane space button, and no group code', () => {
  const toggle = readFileSync(new URL('../src/features/sidebar/FleetViewToggle.tsx', import.meta.url), 'utf8')
  assert.match(toggle, /view: 'lanes', label: 'lanes', title: 'Lanes: who works on what'/)
  const sidebar = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  assert.equal((sidebar.match(/onDragStart:/g) ?? []).length, 1)
  assert.equal((sidebar.match(/onDrop:/g) ?? []).length, 1)
  assert.equal((sidebar.match(/await assignAgent\(/g) ?? []).length, 1)
  assert.equal((sidebar.match(/await createLane\(/g) ?? []).length, 1, 'only a drop on the placeholder creates a lane')
  assert.match(sidebar, /await updateLane\(laneID, \{ lead: name \}\)/)
  assert.match(sidebar, /view === 'lanes' && node\.lane && !node\.lead\s*\? \{ label: 'Make lead'/)
  assert.match(sidebar, /onOpenLaneSpace\(node\.lane!\)/)
  assert.match(sidebar, /aria-label="New lane"/)
  assert.doesNotMatch(sidebar, /pendingGroups|onOpenGroupAsSpace|groupMembers|buildGroupNodes/)
  assert.match(sidebar, /folder && !folded && !groupHeader && <span className="count-badge">/, 'a header carries no second count')
})
