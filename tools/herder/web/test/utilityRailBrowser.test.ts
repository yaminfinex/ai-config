import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import test from 'node:test'

import { createServer } from 'vite'

const execFileAsync = promisify(execFile)

type RailState = {
  centerWidth: number
  railDisplay: string
  railWidth: number
  resizerDisplay: string
  pressed: string | null
}

test('collapsed rails leave computed layout and reopen with status state in sync', { timeout: 30_000 }, async (context) => {
  const server = await createServer({
    root: new URL('..', import.meta.url).pathname,
    logLevel: 'silent',
    server: { host: '127.0.0.1', port: 0 },
  })
  await server.listen()
  const url = server.resolvedUrls?.local[0]
  assert.ok(url, 'Vite must expose the browser-test URL')
  const session = `herder-rail-layout-${process.pid}`
  const browser = async (args: string[]) => {
    const { stdout } = await execFileAsync('agent-browser', ['--session', session, ...args], { encoding: 'utf8' })
    return stdout.trim()
  }
  context.after(async () => {
    try { await browser(['close']) } finally { await server.close() }
  })
  await browser(['open', url])
  await browser(['wait', '--fn', "document.querySelectorAll('.utility-rail').length === 2"])
  await browser(['set', 'viewport', '1280', '800'])

  const state = async (side: 'left' | 'right'): Promise<RailState> => {
    const expression = `(() => {
      const rail = document.querySelector('.utility-rail-${side}')
      const resizer = document.querySelector('.utility-rail-resizer-${side}')
      const center = document.querySelector('.shell-main')
      const toggle = document.querySelector('.rail-status-toggle-${side}')
      return JSON.stringify({
        centerWidth: center.getBoundingClientRect().width,
        railDisplay: getComputedStyle(rail).display,
        railWidth: rail.getBoundingClientRect().width,
        resizerDisplay: getComputedStyle(resizer).display,
        pressed: toggle.getAttribute('aria-pressed'),
      })
    })()`
    const raw = await browser(['eval', '-b', Buffer.from(expression).toString('base64')])
    return JSON.parse(JSON.parse(raw)) as RailState
  }
  const toggle = async (side: 'left' | 'right') => {
    const expression = `document.querySelector('.rail-status-toggle-${side}').click()`
    await browser(['eval', '-b', Buffer.from(expression).toString('base64')])
    await browser(['wait', '50'])
  }

  for (const side of ['left', 'right'] as const) {
    const open = await state(side)
    assert.equal(open.railDisplay, 'flex')
    assert.equal(open.pressed, 'true')
    await toggle(side)
    const collapsed = await state(side)
    assert.equal(collapsed.railDisplay, 'none')
    assert.equal(collapsed.resizerDisplay, 'none')
    assert.equal(collapsed.railWidth, 0)
    assert.equal(collapsed.pressed, 'false')
    assert.ok(collapsed.centerWidth >= open.centerWidth + open.railWidth, `${side} collapse must return rail width to the centre`)
    await toggle(side)
    const reopened = await state(side)
    assert.equal(reopened.railDisplay, 'flex')
    assert.equal(reopened.pressed, 'true')
    assert.equal(reopened.centerWidth, open.centerWidth)
  }
})
