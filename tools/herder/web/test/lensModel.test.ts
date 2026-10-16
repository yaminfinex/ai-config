import assert from 'node:assert/strict'
import test from 'node:test'
import {
  cardLook,
  cardShape,
  crumbParts,
  cycleVisible,
  entryAgent,
  headerLine,
  lensOrder,
  lensRowsOf,
  lensStatus,
  liveMembers,
  looseAgents,
  nextNeeding,
  readToggle,
  revealScroll,
  selectedSpace,
  spaceStep,
  statusText,
  stepCard,
  stepRow,
  visibleAgent,
  whoLine,
  zoomLanding,
  type LensPrefs,
} from '../src/features/lens/lensModel.ts'
import { lensPrefsKey, parseLensPrefs, parseViewedBlocks, readLensPrefs, readViewedBlocks, writeLensPrefs, writeViewedBlocks } from '../src/features/lens/lensPrefs.ts'
import { aloneHistoryEntry, lensHistoryEntry, routeFromLocation, viewFromLocation } from '../src/features/layout/historyModel.ts'
import type { SpaceMember } from '../src/features/spaces/spaceMembersModel.ts'
import type { Board, Pane } from '../src/types.ts'

const spaces = ['a', 'b', 'c', 'd', 'e', 'f'].map((id) => ({ id }))
// focus: b, e · watch: a, c (never placed), f · background: d
const prefs: LensPrefs = { rows: { b: 'focus', e: 'focus', d: 'background', f: 'watch' }, visible: {} }

function pane(agent: string, extra: Partial<Pane> = {}): Pane {
  return { pane_id: `pane-${agent}`, agent, tool: 'claude', herdr_status: 'idle', bus_status: 'listening', gap: '', ...extra }
}

function boardOf(panes: Pane[]): Board {
  return {
    workspaces: [{
      workspace_id: 'w1', number: 1, label: 'fixture', focused: true, pane_count: panes.length, tab_count: 1, active_tab_id: 't1', agent_status: 'idle',
      tabs: [{ tab_id: 't1', number: 1, label: 'agents', focused: true, pane_count: panes.length, agent_status: 'idle', panes }],
    }],
    unplaced: [],
  }
}

const agent = (name: string): SpaceMember => ({ kind: 'agent', name })
const file: SpaceMember = { kind: 'file', root: '/repo', path: 'README.md' }

test('lens order is focus, then watch, then background, each in space order; an unplaced space sits in watch', () => {
  const rows = lensRowsOf(spaces, prefs)
  assert.deepEqual(rows.focus.map(({ id }) => id), ['b', 'e'])
  assert.deepEqual(rows.watch.map(({ id }) => id), ['a', 'c', 'f'])
  assert.deepEqual(rows.background.map(({ id }) => id), ['d'])
  assert.deepEqual(lensOrder(spaces, prefs), ['b', 'e', 'a', 'c', 'f', 'd'])
})

test('the selection falls back to the first card when its space is gone', () => {
  const order = lensOrder(spaces, prefs)
  assert.equal(selectedSpace(order, 'c'), 'c')
  assert.equal(selectedSpace(order, 'gone'), 'b')
  assert.equal(selectedSpace(order, null), 'b')
  assert.equal(selectedSpace([], null), null)
})

test('← → step through lens order across rows and stop at the ends', () => {
  const order = lensOrder(spaces, prefs)
  assert.equal(stepCard(order, 'e', 1), 'a', 'the end of focus steps into watch')
  assert.equal(stepCard(order, 'a', -1), 'e')
  assert.equal(stepCard(order, 'b', -1), 'b')
  assert.equal(stepCard(order, 'd', 1), 'd')
})

test('j k keep the column, else take the row\'s last card, skip empty rows and stay at the edge', () => {
  assert.equal(stepRow(spaces, prefs, 'e', 1), 'c', 'column 2 of focus is column 2 of watch')
  assert.equal(stepRow(spaces, prefs, 'f', -1), 'e', 'column 3 of watch has no focus card: the row\'s last')
  assert.equal(stepRow(spaces, prefs, 'c', 1), 'd')
  assert.equal(stepRow(spaces, prefs, 'd', 1), 'd')
  assert.equal(stepRow(spaces, prefs, 'b', -1), 'b')
  const noWatch: LensPrefs = { rows: { a: 'focus', b: 'background' }, visible: {} }
  assert.equal(stepRow([{ id: 'a' }, { id: 'b' }], noWatch, 'a', 1), 'b', 'an empty row is skipped')
})

test('m marks a card read while it needs the owner, else unread', () => {
  assert.equal(readToggle(1), 'read', 'one unread or blocked')
  assert.equal(readToggle(3), 'read')
  assert.equal(readToggle(0), 'unread', 'nothing needs the owner')
})

test('⇧⌥←/→ step through spaces in lens order and wrap', () => {
  const order = lensOrder(spaces, prefs)
  assert.equal(spaceStep(order, 'd', 1), 'b')
  assert.equal(spaceStep(order, 'b', -1), 'd')
  assert.equal(spaceStep(order, 'e', 1), 'a')
  assert.equal(spaceStep(order, 'gone', 1), 'b')
  assert.equal(spaceStep([], 'b', 1), null)
})

test('the visible agent is the pick while it is a member on the board, else the first member on the board', () => {
  const board = boardOf([pane('mavu'), pane('ziru'), pane('kelo')])
  const live = liveMembers([agent('gone'), file, agent('ziru'), agent('mavu')], board)
  assert.deepEqual(live, ['ziru', 'mavu'], 'files and agents off the board are not live')
  assert.equal(visibleAgent(live, 'mavu'), 'mavu')
  assert.equal(visibleAgent(live, 'gone'), 'ziru')
  assert.equal(visibleAgent([], 'mavu'), undefined)
  assert.equal(cycleVisible(live, undefined), 'mavu')
  assert.equal(cycleVisible(live, 'mavu'), 'ziru', 'v wraps')
  assert.equal(cycleVisible([], undefined), undefined)
  assert.equal(entryAgent(live, 'ziru', (name) => name === 'mavu'), 'mavu', 'a zoom lands on the first member needing you')
  assert.equal(entryAgent(live, 'mavu', () => false), 'mavu', 'else on the visible agent')
})

test('a zoom returns to the panel last focused in the space; the entry agent only without one (owner #463121)', () => {
  const onBoard = (name: string) => ['lead', 'mavu', 'ziru'].includes(name)
  // The herder space: lead first, needing you, but the owner was last on mavu.
  const entry = () => entryAgent(['lead', 'mavu'], undefined, (name) => name === 'lead')
  assert.equal(zoomLanding({ kind: 'agent', name: 'mavu' }, onBoard, entry), 'mavu', 'not the lead, though the lead needs you')
  assert.equal(zoomLanding({ kind: 'agent', name: 'ziru' }, onBoard, entry), 'ziru', 'a preview of a non-member still on the board')
  assert.equal(zoomLanding({ kind: 'file' }, onBoard, entry), undefined, 'a file tab is kept as the layout holds it')
  assert.equal(zoomLanding({ kind: 'agent', name: 'gone' }, onBoard, entry), 'lead', 'an agent no longer on the board falls back')
  assert.equal(zoomLanding(undefined, onBoard, entry), 'lead', 'a space never focused falls back')
  assert.equal(zoomLanding(undefined, onBoard, () => undefined), undefined, 'with no member on the board, nothing opens')
})

test('the who line names the visible agent and counts every other member, files included', () => {
  assert.deepEqual(whoLine([agent('mavu'), file, agent('ziru')], 'mavu'), { name: 'mavu', others: 2 })
  assert.deepEqual(whoLine([file], undefined), { name: '—', others: 1 })
})

test('n stops at the next space needing you in lens order, wrapping to itself last; N adds the agents in no space', () => {
  const order = lensOrder(spaces, prefs)
  const needs = new Set(['c', 'b'])
  const base = { order, loose: looseAgents(['zed', 'lone', 'mavu'], { a: [agent('mavu')] }), spaceNeeds: (id: string) => needs.has(id), agentNeeds: (name: string) => name === 'lone' }
  assert.deepEqual(base.loose, ['lone', 'zed'], 'agents in no space, by name')
  assert.deepEqual(nextNeeding({ ...base, from: { kind: 'space', id: 'b' }, alone: false }), { kind: 'space', id: 'c' })
  assert.deepEqual(nextNeeding({ ...base, from: { kind: 'space', id: 'c' }, alone: false }), { kind: 'space', id: 'b' }, 'n on the lens skips the no-space tail')
  assert.deepEqual(nextNeeding({ ...base, from: { kind: 'space', id: 'c' }, alone: true }), { kind: 'alone', agent: 'lone' }, 'N reaches the tail after the last space')
  assert.deepEqual(nextNeeding({ ...base, from: { kind: 'alone', agent: 'lone' }, alone: true }), { kind: 'space', id: 'b' }, 'and wraps from it')
  assert.deepEqual(nextNeeding({ ...base, from: null, alone: false }), { kind: 'space', id: 'b' })
  assert.deepEqual(nextNeeding({ ...base, spaceNeeds: (id) => id === 'c', from: { kind: 'space', id: 'c' }, alone: false }), { kind: 'space', id: 'c' }, 'from itself last')
  assert.equal(nextNeeding({ ...base, spaceNeeds: () => false, agentNeeds: () => false, from: null, alone: true }), null)
})

test('status: blocked (bus or herdr) over working over done over idle; the fallback body names it', () => {
  assert.equal(lensStatus(pane('a', { bus_status: 'blocked', herdr_status: 'working' })), 'blocked')
  assert.equal(lensStatus(pane('a', { herdr_status: 'blocked' })), 'blocked')
  assert.equal(lensStatus(pane('a', { herdr_status: 'working' })), 'working')
  assert.equal(lensStatus(pane('a', { herdr_status: 'done' })), 'done')
  assert.equal(lensStatus(undefined), 'idle')
  assert.equal(statusText(pane('a', { herdr_status: 'working', title: 'lens-home' }), false), 'working · lens-home')
  assert.equal(statusText(pane('a', { title: 'lens-home' }), true), 'your turn · lens-home')
  assert.equal(statusText(pane('a'), false), 'idle')
  assert.equal(statusText(undefined, false), 'no agent on the board')
})

test('cards: focus at the card size, watch at its width and two lines, background compact; brightness follows needs and work', () => {
  assert.deepEqual(cardShape('focus'), { width: 284, lines: 5 })
  assert.deepEqual(cardShape('watch'), { width: 284, lines: 2 })
  assert.deepEqual(cardShape('background'), { width: 240, lines: 0 })
  assert.deepEqual(cardLook(2, false, 'idle'), { needs: true, selected: false, opacity: 1 })
  assert.deepEqual(cardLook(0, false, 'working'), { needs: false, selected: false, opacity: 0.75 })
  assert.deepEqual(cardLook(0, false, 'done'), { needs: false, selected: false, opacity: 0.5 })
  assert.deepEqual(cardLook(0, true, null), { needs: false, selected: true, opacity: 1 })
})

test('the header line and the crumb', () => {
  assert.equal(headerLine({ live: true, spaces: 3, needYou: 2, serverUpdated: false }), 'herder · live · 3 spaces · 2 need you · ? keys')
  assert.equal(headerLine({ live: false, spaces: 1, needYou: 0, serverUpdated: true }), 'herder · offline · 1 space · 0 need you · server updated · ? keys')
  assert.deepEqual(crumbParts({ kind: 'zoom' }, { name: 'refit' }, 'mavu'), { space: 'refit', agent: 'mavu', alone: false })
  assert.deepEqual(crumbParts({ kind: 'zoom' }, undefined, undefined), { space: '(space gone)', agent: undefined, alone: false })
  assert.deepEqual(crumbParts({ kind: 'alone', agent: 'lone' }, { name: 'refit' }, 'mavu'), { space: 'no space', agent: 'lone', alone: true })
})

test('the selection is revealed with the least scroll', () => {
  const view = { top: 100, bottom: 500 }
  assert.equal(revealScroll(view, { top: 200, bottom: 300 }, 40), 40, 'already whole: no scroll')
  assert.equal(revealScroll(view, { top: 450, bottom: 560 }, 40), 100, 'below: just enough to show its bottom')
  assert.equal(revealScroll(view, { top: 60, bottom: 160 }, 40), 0, 'above: just enough to show its top')
  assert.equal(revealScroll(view, { top: 450, bottom: 1000 }, 0), 350, 'taller than the view: its top')
})

test('lens prefs are a versioned per-browser record that keeps every valid entry', () => {
  assert.deepEqual(parseLensPrefs(JSON.stringify({ version: 1, rows: { a: 'focus', b: 'sideways' }, visible: { a: 'mavu', b: '' } })), { rows: { a: 'focus' }, visible: { a: 'mavu' } })
  assert.equal(parseLensPrefs(JSON.stringify({ version: 2, rows: {}, visible: {} })), null)
  assert.equal(parseLensPrefs('not json'), null)
  const stored = new Map<string, string>()
  const storage = { getItem: (key: string) => stored.get(key) ?? null, setItem: (key: string, value: string) => { stored.set(key, value) } }
  const first = readLensPrefs(storage)
  assert.deepEqual(first.prefs, { rows: {}, visible: {} })
  writeLensPrefs(storage, { rows: { a: 'background' }, visible: { a: 'ziru' } }, first.state)
  assert.ok(stored.has(lensPrefsKey))
  assert.deepEqual(readLensPrefs(storage).prefs, { rows: { a: 'background' }, visible: { a: 'ziru' } })
  assert.deepEqual(parseViewedBlocks(JSON.stringify({ version: 1, agents: ['b', 'a', 'a', 3] })), ['b', 'a'])
  writeViewedBlocks(storage, new Set(['ziru', 'mavu']), readViewedBlocks(storage).state)
  assert.deepEqual([...readViewedBlocks(storage).blocks], ['mavu', 'ziru'])
})

test('the URL names the view: / is the lens, a space or panel a zoom, ?alone an agent alone', () => {
  const view = (path: string, spacesEnabled = true) => {
    const url = new URL(path, 'http://herder.invalid')
    return viewFromLocation(routeFromLocation(url.pathname, url.search), url.search, spacesEnabled)
  }
  assert.deepEqual(view('/'), { kind: 'lens' })
  assert.deepEqual(view('/?space=s1'), { kind: 'zoom' })
  assert.deepEqual(view('/agents/alpha'), { kind: 'zoom' })
  assert.deepEqual(view('/agents/alpha?space=s1'), { kind: 'zoom' })
  assert.deepEqual(view(aloneHistoryEntry('lone').path), { kind: 'alone', agent: 'lone' })
  assert.deepEqual(view('/', false), { kind: 'zoom' }, 'without spaces there is no lens')
  assert.equal(lensHistoryEntry.path, '/')
  assert.deepEqual(lensHistoryEntry.state, { herderLayoutRoute: true, lens: true })
})
