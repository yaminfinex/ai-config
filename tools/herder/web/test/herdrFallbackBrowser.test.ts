import assert from 'node:assert/strict'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import { blockGhosttyWasm, fixtureAPI, herdrSurfaceProbe, startBrowser } from './browserFixture.ts'
import { herdrTermFixture } from './herdrTermFixture.ts'

const viewOpen = `Boolean(document.querySelector('.herdr-view:not([hidden])'))`
const viewState = `(document.querySelector('.herdr-view-state')?.textContent ?? '')`
const screenText = `(window.__surface()?.screen().rows.join('\\n') ?? '')`

// When ghostty-web's WASM cannot load, the herdr view says so in one header
// line and runs on xterm.js instead, against the same client.
test('a herdr view whose ghostty-web WASM fails to load says "loading terminal…", then falls back to xterm.js and says so', { timeout: 120_000 }, async (context) => {
  const api = fixtureAPI(['alpha'])
  const term = await herdrTermFixture(context)
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'herdr-fallback', [api.plugin, term.plugin])
  const scripts = await mkdtemp(join(tmpdir(), 'herder-fallback-'))
  context.after(() => rm(scripts, { recursive: true, force: true }))
  const init = join(scripts, 'init.js')
  await writeFile(init, blockGhosttyWasm(1_500))

  await browser(['--init-script', init, 'open', url])
  await browser(['set', 'viewport', '1200', '800'])
  await waitFor(`document.querySelectorAll('.lens-card').length === 1`)
  await evaluate(herdrSurfaceProbe)
  await browser(['press', 'Control+`'])
  await waitFor(`${viewOpen} && ${viewState} === 'loading terminal…'`)
  assert.equal(await evaluate(`document.querySelector('.herdr-view-state').getAttribute('role')`), 'status')
  assert.equal(term.open(), 0, 'no client before the terminal is up')

  await waitFor(`${viewState} === 'live' && window.__surface()?.kind === 'xterm'`)
  const note = await evaluate(`[...document.querySelectorAll('.herdr-view-note')].map((note) => [note.textContent, note.title])`) as [string, string][]
  assert.deepEqual(note, [['ghostty did not load (blocked by the test); this is xterm.js', 'blocked by the test']], 'one header line says why')
  assert.equal(await evaluate(`document.querySelectorAll('.herdr-view-terminal canvas, .herdr-view-terminal .herdr-ghostty').length`), 0, 'nothing of ghostty-web is left on the host')

  // xterm.js serves the client as before: output, keys, and its caret.
  await waitFor(`${screenText}.includes('hello from herdr')`)
  await browser(['press', 'n'])
  await waitFor(`${screenText}.includes('in 6e')`)
  assert.deepEqual(await term.typed(), ['n'])
  await waitFor(`(() => { const caret = document.querySelector('.herdr-view .xterm-rows.xterm-focus .xterm-cursor.xterm-cursor-block'); return Boolean(caret) && getComputedStyle(caret).backgroundColor === 'rgb(247, 249, 252)' })()`)
})
