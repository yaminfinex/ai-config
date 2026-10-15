import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, herdrSurfaceProbe, startBrowser } from './browserFixture.ts'
import { herdrTermFixture } from './herdrTermFixture.ts'

const viewOpen = `Boolean(document.querySelector('.herdr-view:not([hidden])'))`
const viewState = `(document.querySelector('.herdr-view-state')?.textContent ?? '')`
const screenText = `(window.__surface()?.screen().rows.join('\\n') ?? '')`
const grid = `(() => { const screen = window.__surface().screen(); return [screen.cols, screen.rows.length] })()`
const canvasCentre = `(() => { const r = document.querySelector('.herdr-view-terminal canvas').getBoundingClientRect(); return [Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2)] })()`
const menus = `(() => {
  window.__menus = []
  window.addEventListener('contextmenu', (event) => window.__menus.push({ where: event.target.closest('.herdr-view-terminal') ? 'terminal' : 'app', prevented: event.defaultPrevented }))
  return true
})()`

// The herdr view on ghostty-web against a client in a real PTY (the
// serve's HERDER_TERM_COMMAND contract): what it prints is on the screen,
// what the owner types and clicks reaches it, and the PTY takes the view's size.
test('the herdr view runs on ghostty-web: output, keys, the leave chord, a right-click, resize and the caret reach a client in a real PTY', { timeout: 180_000 }, async (context) => {
  const api = fixtureAPI(['alpha'])
  const term = await herdrTermFixture(context)
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'herdr-ghostty', [api.plugin, term.plugin])
  const press = async (key: string) => { await browser(['press', key]); await browser(['wait', '80']) }
  const until = async (what: string, check: () => Promise<boolean>) => {
    const deadline = Date.now() + 10_000
    while (!(await check())) {
      assert.ok(Date.now() < deadline, `timed out waiting for ${what}`)
      await new Promise((resolve) => setTimeout(resolve, 100))
    }
  }

  await browser(['open', url])
  await browser(['set', 'viewport', '1200', '800'])
  await evaluate('localStorage.clear(), sessionStorage.clear(), true')
  await browser(['reload'])
  await waitFor(`document.querySelectorAll('.lens-card').length === 1`)
  await evaluate(herdrSurfaceProbe)
  await evaluate(menus)
  const ghosttyLoaded = `performance.getEntriesByType('resource').some((entry) => /ghostty-web|ghosttySurface/.test(entry.name))`
  assert.equal(await evaluate(ghosttyLoaded), false, 'ghostty-web is not loaded before the view opens')

  // The view loads ghostty-web, then the client says hello at the view's size.
  await press('Control+`')
  await waitFor(`${viewOpen} && ${viewState} === 'live' && window.__surface()?.kind === 'ghostty'`)
  assert.equal(await evaluate(ghosttyLoaded), true)
  assert.equal(await evaluate(`Boolean(document.querySelector('.herdr-view-note'))`), false, 'no fallback note')
  await waitFor(`${screenText}.includes('hello from herdr')`)
  const [cols, rows] = await evaluate(grid) as [number, number]
  assert.ok(cols > 100 && rows > 30, `the grid fills the view: ${cols}x${rows}`)
  await until('the client to report its first size', async () => (await term.sizes()).length > 0)
  assert.deepEqual((await term.sizes())[0], [cols, rows], 'the PTY starts at the view\'s grid')
  await waitFor(`${screenText}.includes('size ${cols} ${rows}')`)
  assert.equal(await evaluate(`document.activeElement?.closest('.herdr-view-terminal') !== null`), true, 'the terminal has the keys')

  // Keys reach the client as a terminal sends them; ⌥W is ESC w.
  for (const key of ['n', 'ArrowRight', 'Escape', 'Enter', '?', 'Alt+w']) await press(key)
  const expected = 'n\x1b[C\x1b\r?\x1bw'
  await until('the typed keys to reach the client', async () => (await term.typed()).join('').length >= expected.length)
  assert.equal((await term.typed()).join(''), expected)
  await waitFor(`${screenText}.includes('in 1b77')`)

  // ⌃` leaves and comes back without the client hearing it (it would be NUL).
  await press('Control+`')
  await waitFor(`!${viewOpen}`)
  await press('Control+`')
  await waitFor(`${viewOpen} && document.activeElement?.closest('.herdr-view-terminal') !== null`)
  await press('x')
  await until('the next key', async () => (await term.typed()).join('').endsWith('x'))
  assert.equal((await term.typed()).join(''), `${expected}x`, 'nothing reached the client for the chord')

  // A right-click is herdr's: an SGR button-2 press and release, no browser menu.
  const [x, y] = await evaluate(canvasCentre) as [number, number]
  await browser(['mouse', 'move', String(x), String(y)])
  await browser(['mouse', 'down', 'right'])
  await browser(['mouse', 'up', 'right'])
  await until('the right button\'s reports', async () => (await term.typed()).join('').includes('m'))
  const reports = (await term.typed()).join('').slice(expected.length + 1)
  // SGR: ESC [ < button ; column ; row, then M on press and m on release.
  const [lead, pressed, released, extra] = reports.split('\x1b[<')
  assert.deepEqual([lead, extra], ['', undefined], `two reports: ${JSON.stringify(reports)}`)
  const [, column, row] = (/^2;(\d+);(\d+)M$/.exec(pressed) ?? assert.fail(`the press: ${JSON.stringify(reports)}`)).map(Number)
  assert.equal(released, `2;${column};${row}m`, 'the release, where it was pressed')
  assert.ok(Math.abs(column - cols / 2) <= 2 && Math.abs(row - rows / 2) <= 2, `at the cell under the pointer: ${column};${row} of ${cols}x${rows}`)
  assert.deepEqual(await evaluate(`window.__menus`), [{ where: 'terminal', prevented: true }], 'the browser menu is stopped')
  assert.equal(await evaluate(`window.getSelection().toString() + (document.querySelector('.herdr-view-terminal textarea')?.value ?? '')`), '', 'nothing is selected')

  // A smaller window resizes the PTY to the view's new grid.
  await browser(['set', 'viewport', '900', '600'])
  await waitFor(`(() => { const screen = window.__surface().screen(); return screen.cols < ${cols} && screen.rows.length < ${rows} })()`)
  const [smallCols, smallRows] = await evaluate(grid) as [number, number]
  await until('the PTY to take the new size', async () => JSON.stringify((await term.sizes()).at(-1)) === JSON.stringify([smallCols, smallRows]))
  assert.deepEqual(JSON.parse(term.controls.filter((frame) => frame.includes('"resize"')).at(-1)!), { type: 'resize', cols: smallCols, rows: smallRows })
  await waitFor(`${screenText}.includes('size ${smallCols} ${smallRows}')`)

  // The caret is a solid bright block while the view has the keys (wait out its blink).
  await waitFor(`(() => { const caret = window.__caret(); return caret !== null && caret.caret === caret.total })()`)
})
