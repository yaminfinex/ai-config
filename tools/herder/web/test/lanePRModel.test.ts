import assert from 'node:assert/strict'
import test from 'node:test'

import { quickOpenActionRows, quickOpenRowKey, quickOpenTopMatch } from '../src/features/files/quickOpenModel.ts'
import { buildLaneNodes, laneHeaderID } from '../src/features/sidebar/sidebarNodes.ts'
import { parsePRQuery, prFacts, prLanes, prSummary } from '../src/shared/lanePRModel.ts'
import type { Board, LanePR, Row } from '../src/types.ts'

function pr(number: number, extra: Partial<LanePR> = {}): LanePR {
  return { repo: 'acme/app', number, title: `PR ${number}`, url: `https://github.com/acme/app/pull/${number}`, state: 'OPEN', draft: false, unresolved: 0, head: `b-${number}`, ...extra }
}

function agent(name: string): Row {
  return { pane_id: '-', agent: name, tool: 'claude', herdr_status: '-', bus_status: 'listening', gap: 'no visible pane' }
}

const board: Board = {
  workspaces: [],
  unplaced: [agent('lead-a'), agent('impl-a'), agent('lead-b')],
  lanes: [
    { id: 'la', name: 'payload', lead: 'lead-a', members: ['lead-a', 'impl-a'], prs: [
      pr(7, { state: 'MERGED', linked: true }),
      pr(309, { draft: true, checks: 'FAILURE', review: 'REVIEW_REQUIRED', linked: true }),
      pr(314, { checks: 'SUCCESS', unresolved: 1, holder: 'impl-a', head: 'b-314' }),
    ] },
    { id: 'lb', name: 'docs', lead: 'lead-b', members: ['lead-b'], prs: [pr(314, { checks: 'SUCCESS', unresolved: 1 }), pr(12, { repo: 'acme/web', mergeable: 'CONFLICTING', review: 'APPROVED' })], pr_note: undefined },
  ],
}

test('prFacts reads done, then blockers, then draft; an unpolled link says so', () => {
  assert.deepEqual(prFacts(pr(1, { state: 'MERGED', draft: true })), ['merged'])
  assert.deepEqual(prFacts(pr(1, { state: 'CLOSED' })), ['closed'])
  assert.deepEqual(prFacts(pr(1, { mergeable: 'CONFLICTING', checks: 'PENDING', review: 'CHANGES_REQUESTED', unresolved: 2, draft: true })),
    ['conflicts', 'checks running', 'changes requested', '2 unresolved', 'draft'])
  assert.deepEqual(prFacts(pr(1, { checks: 'SUCCESS', review: 'APPROVED' })), ['checks pass', 'approved'])
  assert.deepEqual(prFacts({ ...pr(1), state: '', title: '' }), ['not polled yet'])
  assert.equal(prSummary(pr(9, { checks: 'ERROR' })), '#9 checks failing')
})

test('parsePRQuery takes #n, owner/repo#n and a PR URL, nothing else', () => {
  assert.deepEqual(parsePRQuery(' #309 '), { number: 309 })
  assert.deepEqual(parsePRQuery('Acme/App#309'), { repo: 'acme/app', number: 309 })
  assert.deepEqual(parsePRQuery('https://github.com/acme/app/pull/309/files'), { repo: 'acme/app', number: 309 })
  for (const text of ['309', '#', 'acme/app', 'https://github.com/acme/app/issues/3', 'fix #3']) assert.equal(parsePRQuery(text), null, text)
})

test('prLanes finds every lane holding the PR; a bare number matches any repo', () => {
  assert.deepEqual(prLanes(board, { number: 314 }).map(({ lane }) => lane.id), ['la', 'lb'], 'one PR in two lanes')
  assert.deepEqual(prLanes(board, { repo: 'acme/web', number: 12 }).map(({ lane }) => lane.id), ['lb'])
  assert.deepEqual(prLanes(board, { repo: 'acme/app', number: 12 }), [])
})

test('lane headers list their PR rows first, then the members; the note replaces nothing', () => {
  const nodes = buildLaneNodes(board)
  const header = nodes.get(laneHeaderID('la'))!
  assert.deepEqual(header.children.slice(0, 3), ['lane:la/pr:acme/app#7', 'lane:la/pr:acme/app#309', 'lane:la/pr:acme/app#314'])
  assert.equal(header.summary?.total, 2, 'PR rows are not counted as members')
  const row = nodes.get('lane:la/pr:acme/app#309')!
  assert.equal(row.kind, 'pr')
  assert.equal(row.name, '#309 PR 309')
  assert.equal(row.secondary, 'checks failing, review needed, draft')
  assert.equal(row.lane, 'la', 'a drop on a PR row joins its lane')
  assert.equal(nodes.get('lane:la/pr:acme/app#314')!.secondary, 'checks pass, 1 unresolved, impl-a on b-314', 'the holder hint')
  assert.equal(nodes.get('lane:la/pr:acme/app#7')!.secondary, 'merged')

  const noted = buildLaneNodes({ ...board, lanes: [{ id: 'la', name: 'payload', lead: 'lead-a', members: ['lead-a'], prs: [], pr_note: 'no GitHub token' }] })
  assert.equal(noted.get('lane:la/pr-note')?.name, 'no GitHub token')
  assert.equal(noted.get('lane:la/pr-note')?.pr, undefined)
})

test('a PR typed in the palette lists each lane holding it first and is the top match', () => {
  const rows = quickOpenActionRows('#314', [], ['lead-a'], false, null, undefined, board)
  const prs = rows.filter((row) => row.kind === 'pr')
  assert.deepEqual(prs.map((row) => row.kind === 'pr' && [row.lane, row.lead]), [['la', 'lead-a'], ['lb', 'lead-b']])
  assert.equal(rows[0], prs[0])
  assert.equal(prs[0]!.label, '#314 PR 314 · acme/app · lane payload')
  assert.ok(!rows.some((row) => row.kind === 'create'), 'a PR query offers no "Create space #314"')
  assert.equal(quickOpenTopMatch(rows, null, '#314'), quickOpenRowKey(prs[0]!))
  const url = quickOpenActionRows('https://github.com/acme/web/pull/12', [], [], false, null, undefined, board)
  assert.deepEqual(url.filter((row) => row.kind === 'pr').map((row) => row.kind === 'pr' && row.lane), ['lb'])
  assert.equal(quickOpenActionRows('#999', [], [], false, null, undefined, board).filter((row) => row.kind === 'pr').length, 0)
  assert.equal(quickOpenActionRows('pay', [{ id: 's', name: 'payload', order: 0, created: 0, updated: 0 }], [], false, null, undefined, board)[0]!.kind, 'space')
})
