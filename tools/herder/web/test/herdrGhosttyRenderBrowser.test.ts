import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, herdrSurfaceProbe, startBrowser } from './browserFixture.ts'
import { herdrTermFixture } from './herdrTermFixture.ts'

const viewOpen = `Boolean(document.querySelector('.herdr-view:not([hidden])'))`
const viewState = `(document.querySelector('.herdr-view-state')?.textContent ?? '')`
const screenText = `(window.__surface()?.screen().rows.join('\\n') ?? '')`
const renders = `window.__surface().renders()`
// The colour painted at the centre of a cell of the ghostty canvas.
const cellColour = (col: number, row: number) => `(() => {
  const canvas = document.querySelector('.herdr-view-terminal canvas')
  const screen = window.__surface().screen()
  const width = canvas.width / screen.cols, height = canvas.height / screen.rows.length
  return [...canvas.getContext('2d').getImageData(Math.floor((${col} + 0.5) * width), Math.floor((${row} + 0.5) * height), 1, 1).data.slice(0, 3)]
})()`
// A row of eight cells in one background colour, top left, as a client paints it.
const paintRow = (r: number, g: number, b: number) => `window.__surface().write(new TextEncoder().encode('\\x1b[H\\x1b[48;2;${r};${g};${b}m        \\x1b[0m')), true`

// ghostty-web 0.4.0 renders every animation frame while open, hidden or
// not; the herdr view's ghostty surface renders only when something changed
// (and a blinking caret is a change), and not at all while hidden.
test('the herdr view on ghostty-web renders on demand: none while hidden, only the caret\'s blink while idle, none while blurred, and output still paints', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha'])
  const term = await herdrTermFixture(context)
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'herdr-ghostty-render', [api.plugin, term.plugin])
  const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))
  const rendersOver = async (ms: number) => {
    const before = await evaluate(renders) as number
    await pause(ms)
    return (await evaluate(renders) as number) - before
  }

  await browser(['open', url])
  await browser(['set', 'viewport', '1200', '800'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`document.querySelectorAll('.lens-card').length === 1`)
  await evaluate(herdrSurfaceProbe)
  await browser(['press', 'Control+`'])
  await waitFor(`${viewOpen} && ${viewState} === 'live' && window.__surface()?.kind === 'ghostty'`)
  await waitFor(`${screenText}.includes('hello from herdr')`)
  assert.equal(await evaluate(`document.activeElement?.closest('.herdr-view-terminal') !== null`), true, 'the terminal has the keys')

  // Output paints, within a frame or two.
  await evaluate(paintRow(200, 30, 40))
  await waitFor(`JSON.stringify(${cellColour(3, 0)}) === '[200,30,40]'`)

  // Idle and focused: only the caret's blink (every 530 ms) renders, and it blinks.
  await pause(300)
  const idle = await rendersOver(3_000)
  assert.ok(idle >= 3 && idle <= 8, `idle renders over 3 s are the blink's: ${idle}`)
  const caret = new Set<string>()
  for (let sample = 0; sample < 16 && caret.size < 2; sample++) {
    const seen = await evaluate(`(() => { const caret = window.__caret(); return caret.caret > 0 ? 'lit' : 'off' })()`) as string
    caret.add(seen)
    await pause(130)
  }
  assert.deepEqual([...caret].sort(), ['lit', 'off'], 'the caret blinks')

  // Blurred, with the view still up: the caret holds steady and nothing renders.
  await evaluate(`document.activeElement.blur(), true`)
  await pause(300)
  assert.equal(await rendersOver(2_000), 0, 'no renders while blurred and idle')
  const outline = await evaluate(`window.__caret()`) as { caret: number, total: number }
  assert.ok(outline.caret > 0 && outline.caret < outline.total, `a steady outline: ${JSON.stringify(outline)}`)

  // Hidden: nothing renders, not even for output, which still lands.
  await browser(['press', 'Control+`'])
  await waitFor(`!${viewOpen}`)
  await pause(300)
  const hiddenBefore = await evaluate(renders) as number
  await evaluate(paintRow(30, 160, 60))
  await pause(2_000)
  assert.equal((await evaluate(renders) as number) - hiddenBefore, 0, 'no renders while hidden, output or not')

  // Shown again: the output written while hidden is on screen, and the caret blinks again.
  await browser(['press', 'Control+`'])
  await waitFor(`${viewOpen} && document.activeElement?.closest('.herdr-view-terminal') !== null`)
  await waitFor(`JSON.stringify(${cellColour(3, 0)}) === '[30,160,60]'`)
  await pause(300)
  const shown = await rendersOver(2_000)
  assert.ok(shown >= 2 && shown <= 6, `the blink renders again: ${shown}`)

  // Typing reaches the client, and its echo paints.
  await browser(['press', 'q'])
  await waitFor(`${screenText}.includes('in 71')`)
})
