import assert from 'node:assert/strict'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import { blockGhosttyWasm, fixtureAPI, herdrSurfaceProbe, startBrowser } from './browserFixture.ts'

// A WebSocket standing in for the serve's herdr client: the test pushes
// what the client prints and reads back what the page sends it.
const fakeSocket = `(() => {
  window.__ws = []
  window.WebSocket = class {
    constructor(url) { this.url = url; this.readyState = 0; this.sent = []; window.__ws.push(this); setTimeout(() => { if (this.readyState !== 0) return; this.readyState = 1; this.onopen?.({}) }, 20) }
    send(data) { this.sent.push(typeof data === 'string' ? data : 'bytes:' + new TextDecoder().decode(data)) }
    close() { this.readyState = 3 }
    push(text) { this.onmessage?.({ data: new TextEncoder().encode(text).buffer }) }
  }
  window.__live = () => { const live = window.__ws.filter((socket) => socket.readyState !== 3); return live.length === 1 ? live[0] : null }
  // Every context menu the page sees, where it was and whether it was stopped.
  window.__menus = []
  window.addEventListener('contextmenu', (event) => {
    const where = event.target.closest('.herdr-view-header') ? 'header' : event.target.closest('.herdr-view-terminal') ? 'terminal' : 'app'
    window.__menus.push({ where, prevented: event.defaultPrevented })
  })
  return true
})()`
const live = `window.__live()`
const sent = `(window.__live()?.sent ?? [])`
const viewOpen = `Boolean(document.querySelector('.herdr-view:not([hidden])'))`
const centre = (selector: string) => `(() => { const r = document.querySelector('${selector}').getBoundingClientRect(); return [Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2)] })()`
// A point on the word the client printed at the top left: the first row's
// second cell, on ghostty-web's canvas or xterm.js's first row.
const onWord = `(() => {
  const ghostty = document.querySelector('.herdr-view-terminal canvas')
  const r = ghostty ? ghostty.getBoundingClientRect() : document.querySelector('.herdr-view .xterm-rows > div').getBoundingClientRect()
  const cell = r.width / window.__surface().screen().cols
  const height = ghostty ? r.height / window.__surface().screen().rows.length : r.height
  return [Math.round(r.left + cell * 1.5), Math.round(r.top + height / 2)]
})()`

// On a Mac xterm.js handles the right button on mousedown and, by default,
// selects the word under it; the page sees a Mac from its first load. Each
// surface is checked: ghostty-web, and xterm.js as the fallback it uses
// when ghostty-web's WASM cannot load.
for (const surface of ['ghostty', 'xterm'] as const) for (const platform of ['Linux', 'Mac'] as const) test(`a right-click in the herdr view on ${surface} is herdr's on ${platform}: no browser menu, a button-2 report, and the rest of herder keeps its menu`, { timeout: 120_000 }, async (context) => {
  const api = fixtureAPI(['alpha'])
  const { url, browser, evaluate, waitFor } = await startBrowser(context, `herdr-rightclick-${surface}-${platform}`, api.plugin)
  const scripts = await mkdtemp(join(tmpdir(), 'herder-rightclick-'))
  context.after(() => rm(scripts, { recursive: true, force: true }))
  const init = join(scripts, 'init.js')
  await writeFile(init, [
    platform === 'Mac' ? `Object.defineProperty(Navigator.prototype, 'platform', { get: () => 'MacIntel' })` : '',
    surface === 'xterm' ? blockGhosttyWasm() : '',
  ].join(';\n'))
  const rightClick = async (point: string) => {
    const [x, y] = await evaluate(point) as [number, number]
    await browser(['mouse', 'move', String(x), String(y)])
    await browser(['mouse', 'down', 'right'])
    await browser(['mouse', 'up', 'right'])
    await browser(['wait', '80'])
  }

  await browser(['--init-script', init, 'open', url])
  assert.equal(platform === 'Mac', await evaluate(`navigator.platform === 'MacIntel'`))
  await browser(['set', 'viewport', '1200', '800'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`document.querySelectorAll('.lens-card').length === 1`)
  await evaluate(fakeSocket)
  await evaluate(herdrSurfaceProbe)
  await browser(['press', 'Control+`'])
  await waitFor(`${viewOpen} && ${live} !== null && (document.querySelector('.herdr-view-state')?.textContent ?? '') === 'live'`)
  assert.equal(await evaluate(`window.__surface().kind`), surface)

  // The client turns on mouse tracking with SGR reports, as herdr does.
  await evaluate(`${live}.push('\\x1b[?1000h\\x1b[?1006hready'), true`)
  await waitFor(`window.__surface().screen().rows[0].includes('ready')`)
  if (surface === 'xterm') await waitFor(`document.querySelector('.herdr-view .xterm').classList.contains('enable-mouse-events')`)
  const before = await evaluate(`${sent}.length`) as number
  await rightClick(onWord)
  const reports = (await evaluate(`${sent}.slice(${before})`) as string[]).filter((frame) => frame.startsWith('bytes:')).map((frame) => frame.slice(6))
  // SGR reports: ESC [ < button ; column ; row, then M on press and m on release.
  const sgr = (report: string) => report.startsWith('\x1b[<') ? /^2;\d+;\d+[Mm]$/.exec(report.slice(3))?.[0].at(-1) : undefined
  assert.ok(reports.some((report) => sgr(report) === 'M'), `herdr gets the right button's press: ${JSON.stringify(reports)}`)
  assert.ok(reports.some((report) => sgr(report) === 'm'), `and its release: ${JSON.stringify(reports)}`)
  assert.deepEqual(await evaluate(`window.__menus`), [{ where: 'terminal', prevented: true }], 'the browser menu is stopped on the terminal')
  // Both hand a right-click's selection to their textarea for the browser menu.
  assert.equal(await evaluate(`document.querySelector('.herdr-view-terminal textarea').value + window.getSelection().toString()`), '', 'the word under it is not selected')

  // The view's header is herder's own chrome: its menu is untouched.
  await rightClick(centre('.herdr-view-header strong'))
  assert.deepEqual(await evaluate(`window.__menus.at(-1)`), { where: 'header', prevented: false })

  // So is the rest of the app once the view is left.
  await browser(['press', 'Control+`'])
  await waitFor(`!${viewOpen}`)
  await rightClick(centre('.lens-card'))
  assert.deepEqual(await evaluate(`window.__menus.at(-1)`), { where: 'app', prevented: false })
})
