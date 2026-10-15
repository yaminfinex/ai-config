import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

// A WebSocket that records what the page sends and lets the test speak for
// the serve. Vite's own HMR socket is already open, so only the herdr view's
// sockets land here.
const fakeSocket = `(() => {
  window.__ws = []
  window.WebSocket = class {
    constructor(url) { this.url = url; this.readyState = 0; this.sent = []; this.closedByPage = null; window.__ws.push(this); setTimeout(() => { if (this.readyState !== 0) return; this.readyState = 1; this.onopen?.({}) }, 20) }
    send(data) { this.sent.push(typeof data === 'string' ? data : 'bytes:' + new TextDecoder().decode(data)) }
    close(code) { this.closedByPage = code; this.readyState = 3 }
    push(text) { this.onmessage?.({ data: new TextEncoder().encode(text).buffer }) }
    drop() { this.readyState = 3; this.onclose?.({}) }
  }
  // The one socket the page keeps open (StrictMode's dev double mount opens
  // and closes one first).
  window.__live = () => { const live = window.__ws.filter((socket) => socket.readyState !== 3); return live.length === 1 ? live[0] : null }
  return true
})()`
const live = `window.__live()`
const sent = `(window.__live()?.sent ?? [])`
const focusSent = (pane: string) => `${sent}.includes('{"type":"focus","pane_id":"${pane}"}')`
const viewOpen = `Boolean(document.querySelector('.herdr-view:not([hidden])'))`
const viewState = `(document.querySelector('.herdr-view-state')?.textContent ?? '')`
const selectedAgent = `(document.querySelector('.lens-card.selected .lens-agent')?.textContent ?? null)`
const cardOf = (agent: string) => `[...document.querySelectorAll('.lens-card')].find((card) => card.querySelector('.lens-agent')?.textContent === '${agent}')`

test('the herdr view opens and leaves by chord and control, owns the keys, resizes, reconnects and follows the agent', { timeout: 240_000 }, async (context) => {
  const api = fixtureAPI(['alpha', 'bravo'])
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'herdr-view', api.plugin)
  const press = async (key: string) => { await browser(['press', key]); await browser(['wait', '80']) }

  // Two spaces, alpha's and bravo's, so the lens has a card for each.
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1400', '900'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message alpha"]'))`)
  await evaluate(`document.querySelector('.dv-active-group .herder-dock-tab.preview')?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await evaluate(`document.querySelector('.space-create').click(), true`)
  await waitFor(`document.querySelectorAll('.spaces-list .space-row').length === 2`)
  await evaluate(`[...document.querySelectorAll('[role="treeitem"]')].find((node) => node.textContent.includes('bravo')).dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await waitFor(`Boolean(document.querySelector('textarea[data-composer][aria-label="Message bravo"]'))`)
  await browser(['open', url])
  await waitFor(`document.querySelectorAll('.lens-card').length === 2`)
  await evaluate(fakeSocket)
  await evaluate(`${cardOf('alpha')}.click(), true`)
  await waitFor(`${selectedAgent} === 'alpha'`)
  assert.equal(await evaluate(`Boolean(document.querySelector('.herdr-view'))`), false, 'nothing mounts or connects before the owner asks')

  // ⌃` opens it over the whole app, at the browser's grid, and herdr is asked to show alpha.
  await press('Control+`')
  await waitFor(`${viewOpen} && ${live} !== null && ${viewState} === 'live'`)
  await evaluate(`window.__first = ${live}, true`)
  const first = await evaluate(`${live}.url`) as string
  const grid = new URL(first)
  assert.equal(grid.pathname, '/api/herdr/term')
  assert.ok(Number(grid.searchParams.get('cols')) > 100 && Number(grid.searchParams.get('rows')) > 30, `the first size is the view's: ${first}`)
  assert.deepEqual(await evaluate(`(() => { const r = document.querySelector('.herdr-view').getBoundingClientRect(); return [r.left, r.top, r.width, r.height] })()`), [0, 0, 1400, 900], 'full size over the app')
  await waitFor(focusSent('w1:p1'))
  assert.match(await evaluate(`document.querySelector('.herdr-view-header').textContent`) as string, /⌃`|Ctrl\+`/, 'the header names the leave chord')
  assert.equal(await evaluate(`document.activeElement?.closest('.herdr-view .xterm') !== null`), true, 'the terminal has the keys')

  // Output is drawn; keys go to herdr and the lens and shell keys stay inert.
  await evaluate(`${live}.push('hello from herdr'), true`)
  await waitFor(`document.querySelector('.herdr-view .xterm-rows')?.textContent.includes('hello from herdr')`)
  const before = await evaluate(`${sent}.length`) as number
  for (const key of ['n', 'ArrowRight', 'Escape', 'Enter', '?', '/', 'Alt+w']) await press(key)
  const typed = (await evaluate(`${sent}.slice(${before})`) as string[]).filter((frame) => frame.startsWith('bytes:')).map((frame) => frame.slice(6))
  assert.deepEqual(typed, ['n', '\x1b[C', '\x1b', '\r', '?', '/', '\x1bw'], 'each key is herdr\'s, ⌥W (close pane) included')
  assert.equal(await evaluate(selectedAgent), 'alpha', 'the lens did not move')
  assert.equal(await evaluate(`Boolean(document.querySelector('.shortcut-reference'))`), false, '? did not open the reference')
  assert.equal(await evaluate(viewOpen), true, 'Escape stays herdr\'s')

  // A smaller window is a resize frame, after the debounce.
  await browser(['set', 'viewport', '1000', '700'])
  await waitFor(`${sent}.some((frame) => frame.startsWith('{"type":"resize"') && JSON.parse(frame).cols < ${grid.searchParams.get('cols')})`)

  // A drop reconnects with backoff, says so, and re-sends the follow.
  await evaluate(`${live}.drop(), true`)
  await waitFor(`${viewState}.startsWith('reconnecting')`)
  assert.equal(await evaluate(`document.querySelector('.herdr-view-state').getAttribute('role')`), 'status')
  await waitFor(`${live} !== null && ${live} !== window.__first && ${viewState} === 'live' && ${focusSent('w1:p1')}`)
  assert.ok((await evaluate(`${live}.url`) as string).includes('cols='), 'the new client starts at the current size')
  await evaluate(`window.__second = ${live}, true`)

  // The same chord leaves; the lens has its keys back; the client is kept for the grace.
  await press('Control+`')
  await waitFor(`!${viewOpen}`)
  assert.equal(await evaluate(`window.__second.closedByPage === null && ${live} === window.__second`), true, 'leaving keeps the client')
  await press('ArrowRight')
  await waitFor(`${selectedAgent} === 'bravo'`)

  // The status bar's control opens it again on the same client, now following bravo; × leaves.
  await evaluate(`document.querySelector('.herdr-button').click(), true`)
  await waitFor(`${viewOpen} && ${focusSent('w1:p2')}`)
  assert.equal(await evaluate(`${live} === window.__second`), true, 'the same client, within the grace')
  await evaluate(`document.querySelector('.herdr-view-leave').click(), true`)
  await waitFor(`!${viewOpen}`)

  // Follow off: a new agent sends no switch.
  await evaluate(`document.querySelector('.herdr-button').click(), true`)
  await waitFor(viewOpen)
  await evaluate(`document.querySelector('.herdr-view-follow input').click(), true`)
  await press('Control+`')
  await waitFor(`!${viewOpen}`)
  await press('ArrowLeft')
  await waitFor(`${selectedAgent} === 'alpha'`)
  const sentBefore = await evaluate(`${sent}.filter((frame) => frame.includes('"focus"')).length`) as number
  await press('Control+`')
  await waitFor(viewOpen)
  await browser(['wait', '300'])
  assert.equal(await evaluate(`${sent}.filter((frame) => frame.includes('"focus"')).length`), sentBefore, 'follow off moves nothing')
})
