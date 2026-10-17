import assert from 'node:assert/strict'
import type { IncomingMessage, ServerResponse } from 'node:http'
import test, { type TestContext } from 'node:test'

import type { Plugin } from 'vite'

import { fixtureAPI, lanesAPI, startBrowser } from './browserFixture.ts'

// Shared page probes. A tree row is found by its label; a lane space by its id.
const row = (label: string) => `[...document.querySelectorAll('[role="treeitem"]')].find((node) => node.querySelector('.tree-label')?.textContent.trim().startsWith(${JSON.stringify(label)}))`
const spaceRow = (id: string) => `document.querySelector('.spaces-list [data-space-id="${id}"]')`
const spaceIDs = `[...document.querySelectorAll('.spaces-list [data-space-id]')].map((node) => node.dataset.spaceId)`
const composer = (name: string) => `document.querySelector('.dv-active-group textarea[data-composer][aria-label="Message ${name}"]')`
const onLens = `Boolean(document.querySelector('.lens'))`
const layoutKey = (id: string) => `herder.web.layout.v4:${encodeURIComponent(id)}`
const card = (id: string) => `document.querySelector('.lens-card[data-space-id="${id}"]')`

// drag dispatches the native drag sequence the tree listens to, one event
// per call so React commits the drag source before the drop reads it.
async function drag(evaluate: (expression: string) => Promise<unknown>, source: string, target: string) {
  await evaluate(`(window.__drag = new DataTransfer(), ${source}.dispatchEvent(new DragEvent('dragstart', { bubbles: true, dataTransfer: window.__drag })), true)`)
  await evaluate(`(${target}.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: window.__drag })), true)`)
  await evaluate(`(${target}.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: window.__drag })), ${source}?.dispatchEvent(new DragEvent('dragend', { bubbles: true })), true)`)
}

test('lanes are spaces: they appear live in the sidebar and lens, rename the lane, take drops, grow by "+ lane", and hide when empty', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo', 'charlie', 'delta', 'echo'])
  // alpha is listed before the lead bravo: the lens must still show bravo.
  const lanes = lanesAPI(api, [{ id: 'pay1', name: 'payload', lead: 'bravo', members: ['alpha', 'bravo'] }])
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'lanes-sidebar', [lanes.plugin, api.plugin])
  const posts = () => lanes.requests.filter((request) => request.method === 'POST')

  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1280', '800'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`Boolean(${composer('alpha')})`)
  await evaluate(`document.querySelector('.fleet-view-option[title^="Lanes"]').click(), true`)
  await waitFor(`Boolean(${row('payload')})`)
  const ids = JSON.parse(await evaluate(`JSON.stringify(${spaceIDs})`) as string) as string[]
  assert.equal(ids[0], 'lane:pay1', 'a lane space is listed ahead of the user\'s own')
  assert.equal(ids.length, 2)
  assert.match(await evaluate(`${row('payload')}.textContent`) as string, /lead bravo/, 'the header marks the lead')
  assert.equal(await evaluate(`${spaceRow('lane:pay1')}.querySelector('.space-close') === null && ${spaceRow('lane:pay1')}.draggable === false`), true, 'a lane space has no close and no drag')

  // A new lane appears in the sidebar and the Spaces list without a reload.
  lanes.add({ id: 'docs1', name: 'docs', lead: 'charlie', members: ['delta', 'charlie'] })
  await waitFor(`Boolean(${row('docs')}) && Boolean(${spaceRow('lane:docs1')})`)

  // On the lens a lane card shows its lead, and a lane added there appears too.
  await evaluate(`${composer('alpha')}.focus(), true`)
  await browser(['press', 'Escape'])
  await browser(['press', 'Escape'])
  await waitFor(`${onLens} && Boolean(${card('lane:pay1')}) && Boolean(${card('lane:docs1')})`)
  assert.equal(await evaluate(`${card('lane:pay1')}.querySelector('.lens-agent').textContent`), 'bravo', 'the lead, not the first member')
  assert.equal(await evaluate(`${card('lane:docs1')}.querySelector('.lens-agent').textContent`), 'charlie')
  lanes.add({ id: 'web1', name: 'web', lead: 'echo', members: ['echo'] })
  await waitFor(`Boolean(${card('lane:web1')}) && ${card('lane:web1')}.querySelector('.lens-agent').textContent === 'echo'`)
  const own = JSON.parse(await evaluate(`JSON.stringify([...document.querySelectorAll('.lens-card[data-space-id]')].map((node) => node.dataset.spaceId).filter((id) => !id.startsWith('lane:')))`) as string) as string[]
  assert.equal(own.length, 1)
  await evaluate(`${card(own[0]!)}.dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await waitFor(`!${onLens} && Boolean(${row('payload')})`)

  // A drop moves a seat: alpha onto the docs header.
  await drag(evaluate, row('alpha'), row('docs'))
  await waitFor(`${row('docs')}.getAttribute('aria-expanded') !== null && [...document.querySelectorAll('[role="treeitem"]')].filter((node) => node.textContent.includes('alpha')).length === 1`)
  assert.deepEqual(posts().at(-1), { method: 'POST', path: '/api/agents/alpha/assignment', body: { lane: 'docs1' } })
  assert.deepEqual(lanes.record.find((lane) => lane.id === 'docs1')?.members, ['delta', 'charlie', 'alpha'])

  // "+ lane" takes a name; the first agent dropped on it is the lead. bravo
  // leaves payload, which then has no member: its header and space go.
  await evaluate(`document.querySelector('[aria-label="New lane"]').click(), true`)
  await waitFor(`Boolean(document.querySelector('[aria-label="New lane name"]'))`)
  await browser(['fill', '[aria-label="New lane name"]', 'docs'])
  await browser(['press', 'Enter'])
  await waitFor(`Boolean(document.querySelector('.group-create-problem'))`)
  assert.match(await evaluate(`document.querySelector('.group-create-problem').textContent`) as string, /lane docs already exists/)
  await browser(['fill', '[aria-label="New lane name"]', 'ops'])
  await browser(['press', 'Enter'])
  await waitFor(`Boolean(${row('ops')})`)
  assert.equal(posts().length, 1, 'naming a lane writes nothing')
  await drag(evaluate, row('bravo'), row('ops'))
  await waitFor(`Boolean(${row('ops')}) && ${row('ops')}.textContent.includes('lead bravo')`)
  assert.deepEqual(posts().at(-1), { method: 'POST', path: '/api/lanes', body: { name: 'ops', lead: 'bravo' } })
  await waitFor(`!${row('payload')} && !${spaceRow('lane:pay1')} && Boolean(${spaceRow('lane:lane1')})`)
  assert.equal(await evaluate(`[...document.querySelectorAll('[role="treeitem"]')].filter((node) => node.textContent.includes('ops')).length`), 1, 'the placeholder header is gone')

  // A row's context menu makes it the lead.
  await evaluate(`${row('alpha')}.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 40, clientY: 200 })), true`)
  await waitFor(`[...document.querySelectorAll('[role="menuitem"]')].some((item) => item.textContent === 'Make lead')`)
  await evaluate(`[...document.querySelectorAll('[role="menuitem"]')].find((item) => item.textContent === 'Make lead').click(), true`)
  await waitFor(`${row('docs')}.textContent.includes('lead alpha')`)
  assert.deepEqual(posts().at(-1), { method: 'POST', path: '/api/lanes/docs1', body: { lead: 'alpha' } })

  // Renaming the lane space from the sidebar renames the lane; the name shown is the board's.
  await evaluate(`${spaceRow('lane:docs1')}.querySelector('.space-name').click(), true`)
  await waitFor(`${spaceRow('lane:docs1')}.querySelector('.space-name').getAttribute('aria-current') === 'true'`)
  await evaluate(`${spaceRow('lane:docs1')}.querySelector('.space-name').dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await waitFor(`Boolean(document.querySelector('input[aria-label="Rename docs"]'))`)
  await browser(['fill', 'input[aria-label="Rename docs"]', 'docs-2'])
  await browser(['press', 'Enter'])
  await waitFor(`${spaceRow('lane:docs1')}?.querySelector('.space-name')?.textContent.trim() === 'docs-2' && Boolean(${row('docs-2')})`)
  assert.deepEqual(posts().at(-1), { method: 'POST', path: '/api/lanes/docs1', body: { name: 'docs-2' } })

  // A lane whose last member ends leaves the sidebar and the lens.
  lanes.drop('echo')
  await waitFor(`!${spaceRow('lane:web1')} && !${row('web')}`)
})

type Middleware = (request: IncomingMessage, response: ServerResponse, next: () => void) => void

// oldSpace builds two own spaces, the second ("old") holding bravo (and,
// with split, alpha beside it in a second group), gives old a lens tier,
// and waits for its members row on the serve. before runs ahead of the
// fixture on every request.
async function oldSpace(context: TestContext, name: string, options: { agents?: string[], split?: boolean, before?: Middleware } = {}) {
  const api = fixtureAPI(options.agents ?? ['alpha', 'bravo'])
  const lanes = lanesAPI(api, [{ id: 'pay1', name: 'payload', lead: 'bravo', members: ['bravo'] }])
  const before: Plugin = { name: 'lanes-before', configureServer(server) { if (options.before) server.middlewares.use(options.before) } }
  const { url, browser, evaluate, waitFor } = await startBrowser(context, name, [before, lanes.plugin, api.plugin])
  const members = () => api.state.get('spaces.members')
  const spaces = () => api.state.get('spaces')
  const open = (label: string, altKey = false) => evaluate(`(() => {
    const row = [...document.querySelectorAll('[role="treeitem"]')].find((node) => node.querySelector('.tree-label')?.textContent.trim().startsWith(${JSON.stringify(label)}))
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, altKey: ${altKey} }))
    return true
  })()`)

  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1280', '800'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`Boolean(${composer('alpha')})`)
  await evaluate(`document.querySelector('.space-create').click(), true`)
  await waitFor(`document.querySelectorAll('.spaces-list [data-space-id]:not([data-lane])').length === 2`)
  await open('bravo')
  await waitFor(`Boolean(${composer('bravo')})`)
  if (options.split) {
    await open('alpha', true)
    await waitFor(`document.querySelectorAll('.dv-groupview').length === 2`)
  }
  const old = await evaluate(`new URLSearchParams(location.search).get('space')`) as string
  assert.ok(old && !old.startsWith('lane:'))
  await waitFor(`Boolean(localStorage.getItem(${JSON.stringify(layoutKey(old))}))`)
  // A tier for the old space, as the lens's t would store it.
  await evaluate(`(() => {
    const prefs = JSON.parse(localStorage.getItem('herder.web.lens.v1') ?? '{"version":1}')
    prefs.rows = { ...(prefs.rows ?? {}), ${JSON.stringify(old)}: 'focus' }
    localStorage.setItem('herder.web.lens.v1', JSON.stringify(prefs))
    return true
  })()`)
  const want = options.split ? 2 : 1
  const deadline = Date.now() + 10_000
  const published = () => ((members()?.get(old)?.row as unknown as { value?: { members?: unknown[] } } | undefined)?.value?.members?.length ?? 0) >= want
  while (!published() && Date.now() < deadline) await browser(['wait', '200'])
  assert.ok(published(), 'the old space published its members row')
  return { api, lanes, url, browser, evaluate, waitFor, members, spaces, old }
}

test('the lane move re-keys the members row, layout and tier of an old space to its lane once, and never again', { timeout: 180_000 }, async (context) => {
  const { api, lanes, browser, evaluate, waitFor, members, spaces, old } = await oldSpace(context, 'lanes-move')

  // The move script maps the old space to the payload lane; the reload applies it.
  assert.ok(lanes.requests.some((request) => request.path === '/api/lanes/moves'), 'the first load asked and found no moves')
  assert.equal(await evaluate(`localStorage.getItem('herder.web.laneMove.v1')`), null)
  lanes.setMoves({ [old]: 'pay1' })
  await browser(['reload'])
  await waitFor(`localStorage.getItem('herder.web.laneMove.v1')?.includes(${JSON.stringify(old)}) ?? false`)
  await waitFor(`!${spaceRow(old)} && Boolean(${spaceRow('lane:pay1')})`)
  await waitFor(`new URLSearchParams(location.search).get('space') === 'lane:pay1'`)
  assert.equal(await evaluate(`localStorage.getItem(${JSON.stringify(layoutKey(old))})`), null, 'the layout left the old key')
  assert.ok(await evaluate(`localStorage.getItem(${JSON.stringify(layoutKey('lane:pay1'))})`), 'and is the lane space\'s')
  const lens = JSON.parse(await evaluate(`localStorage.getItem('herder.web.lens.v1')`) as string) as { rows?: Record<string, string> }
  assert.equal(lens.rows?.['lane:pay1'], 'focus')
  assert.equal(lens.rows?.[old], undefined)
  const rowsDeadline = Date.now() + 10_000
  while ((!members()?.has('lane:pay1') || !(members()?.get(old)?.row as unknown as { deleted?: boolean }).deleted) && Date.now() < rowsDeadline) await browser(['wait', '200'])
  const moved = members()?.get('lane:pay1')?.row as unknown as { value?: { members?: { name: string }[] } } | undefined
  assert.deepEqual(moved?.value?.members?.map((member) => member.name), ['bravo'], 'the old space\'s members row is the lane space\'s')
  const oldMembers = members()?.get(old)?.row as unknown as { deleted?: boolean }
  assert.equal(oldMembers?.deleted, true, 'the old members row is removed')
  const oldRow = spaces()?.get(old)?.row as unknown as { deleted?: boolean }
  assert.equal(oldRow?.deleted, true, 'the old space row is tombstoned')
  assert.ok(await evaluate(`Boolean(${composer('bravo')})`), 'the lane space opens on the moved dock')

  // Never again: a later reload with the same moves and a new own layout under the old id changes nothing.
  const posted = api.posts.length
  await evaluate(`localStorage.setItem('herder.web.lens.v1', JSON.stringify({ ...JSON.parse(localStorage.getItem('herder.web.lens.v1')), rows: { ...JSON.parse(localStorage.getItem('herder.web.lens.v1')).rows, ${JSON.stringify(old)}: 'watch' } })), true`)
  await browser(['reload'])
  await waitFor(`Boolean(${spaceRow('lane:pay1')}) && Boolean(${composer('bravo')})`)
  await browser(['wait', '1500'])
  const after = JSON.parse(await evaluate(`localStorage.getItem('herder.web.lens.v1')`) as string) as { rows?: Record<string, string> }
  assert.equal(after.rows?.[old], 'watch', 'the tier under the old id is left alone')
  assert.equal(after.rows?.['lane:pay1'], 'focus')
  assert.equal(api.posts.slice(posted).some((post) => post.namespace === 'spaces' && post.keys.includes(old)), false, 'no second tombstone')
})

test('the lane move waits for a full members pull: pins another device added survive a failed first pull', { timeout: 180_000 }, async (context) => {
  let failUntil = 0
  const { lanes, browser, waitFor, members, old } = await oldSpace(context, 'lanes-move-retry', {
    agents: ['alpha', 'bravo', 'charlie'],
    before: (request, response, next) => {
      if (Date.now() < failUntil && request.method === 'GET' && request.url?.startsWith('/api/state/spaces.members')) {
        response.statusCode = 503
        response.end('{}')
      } else next()
    },
  })
  // Another device pinned charlie in the old space; this browser's cache holds only bravo.
  const previous = members()!.get(old)!
  const row = previous.row as unknown as { value: { members: unknown[] } }
  members()!.set(old, { rev: previous.rev + 1, row: { ...row, value: { ...row.value, members: [...row.value.members, { kind: 'agent', name: 'charlie' }] }, writeID: 'remote-add', updated: Date.now() } as never })
  lanes.setMoves({ [old]: 'pay1' })
  failUntil = Date.now() + 2000
  await browser(['reload'])
  await waitFor(`localStorage.getItem('herder.web.laneMove.v1')?.includes(${JSON.stringify(old)}) ?? false`)
  assert.ok(Date.now() >= failUntil, 'the move ran only after the members pull recovered')
  const deadline = Date.now() + 10_000
  const names = () => ((members()?.get('lane:pay1')?.row as unknown as { value?: { members?: { name: string }[] } } | undefined)?.value?.members ?? []).map((member) => member.name)
  const oldDeleted = () => (members()?.get(old)?.row as unknown as { deleted?: boolean }).deleted === true
  while ((!names().includes('charlie') || !oldDeleted()) && Date.now() < deadline) await browser(['wait', '200'])
  assert.deepEqual(names(), ['bravo', 'charlie'])
  assert.equal(oldDeleted(), true, 'the old members row is removed')
})

test('a second browser moves its own dock even when another browser already moved the shared rows', { timeout: 180_000 }, async (context) => {
  let slow = false
  const { lanes, browser, evaluate, waitFor, members, spaces, old } = await oldSpace(context, 'lanes-move-second', {
    split: true,
    before: (request, _response, next) => {
      if (slow && request.url?.startsWith('/api/lanes/moves')) setTimeout(next, 1800)
      else next()
    },
  })
  // Another browser already moved the members row and tombstoned the old
  // space; this one still has its two-group dock under the old id.
  const oldMembers = members()!.get(old)!
  members()!.set('lane:pay1', { rev: oldMembers.rev + 10, row: { ...oldMembers.row, key: 'lane:pay1', writeID: 'remote-lane-members', updated: Date.now() + 100 } as never })
  members()!.set(old, { rev: oldMembers.rev + 11, row: { ...oldMembers.row, deleted: true, writeID: 'remote-old-members', updated: Date.now() + 100 } as never })
  const oldSpaceRow = spaces()!.get(old)!
  spaces()!.set(old, { rev: oldSpaceRow.rev + 12, row: { ...oldSpaceRow.row, deleted: true, writeID: 'remote-old-space', updated: Date.now() + 100 } as never })
  lanes.setMoves({ [old]: 'pay1' })
  slow = true
  const before = await evaluate(`localStorage.getItem(${JSON.stringify(layoutKey(old))})`) as string
  assert.equal(JSON.parse(before).dock.grid.root.data.length, 2)
  await browser(['reload'])
  await waitFor(`localStorage.getItem('herder.web.laneMove.v1')?.includes(${JSON.stringify(old)}) ?? false`)
  await waitFor(`new URLSearchParams(location.search).get('space') === 'lane:pay1'`)
  assert.equal(await evaluate(`localStorage.getItem(${JSON.stringify(layoutKey(old))})`), null, 'the dock left the old key')
  const moved = await evaluate(`localStorage.getItem(${JSON.stringify(layoutKey('lane:pay1'))})`) as string
  assert.equal(JSON.parse(moved).dock.grid.root.data.length, 2, 'the lane space has this browser\'s two-group dock')
  await waitFor(`document.querySelectorAll('.dv-groupview').length === 2 && Boolean(document.querySelector('textarea[data-composer][aria-label="Message bravo"]'))`)
})
