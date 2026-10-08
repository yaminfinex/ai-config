import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import type { ServerResponse } from 'node:http'
import { promisify } from 'node:util'
import test from 'node:test'

import { createServer, type Plugin } from 'vite'

const execFileAsync = promisify(execFile)

// Just enough of the serve API for two live agents with enabled composers.
const agents = ['alpha', 'bravo']
const row = (agent: string, index: number) => ({
  pane_id: `w1:p${index + 1}`, agent, tool: 'claude', herdr_status: 'idle', bus_status: 'listening', gap: '',
})
const board = {
  workspaces: [{
    workspace_id: 'w1', number: 1, label: 'fixture', focused: true, pane_count: agents.length, tab_count: 1, active_tab_id: 't1', agent_status: 'idle',
    tabs: [{ tab_id: 't1', number: 1, label: 'main', focused: true, pane_count: agents.length, agent_status: 'idle', panes: agents.map(row) }],
  }],
  unplaced: [],
}

function fixtureAPI(): Plugin {
  const streams = new Set<ServerResponse>()
  // The serve's /api/state store: every posted row is accepted under a rising rev.
  const state = new Map<string, Map<string, { rev: number, row: { key: string } }>>()
  let rev = 0
  return {
    name: 'quick-open-focus-fixture-api',
    configureServer(server) {
      server.httpServer?.on('close', () => { for (const stream of streams) stream.end() })
      server.middlewares.use((request, response, next) => {
        const url = new URL(request.url ?? '/', 'http://fixture')
        if (!url.pathname.startsWith('/api/')) return next()
        const json = (body: unknown, status = 200) => {
          response.statusCode = status
          response.setHeader('Content-Type', 'application/json')
          response.end(JSON.stringify(body))
        }
        if (url.pathname === '/api/events') {
          response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
          response.write(': fixture\n\n')
          streams.add(response)
          request.on('close', () => streams.delete(response))
          return
        }
        if (url.pathname === '/api/fleet') return json(board)
        if (url.pathname === '/api/viewer') return json({ viewer: 'web-owner' })
        const namespace = url.pathname.match(/^\/api\/state\/([^/]+)$/)?.[1]
        if (namespace) {
          const rows = state.get(namespace) ?? new Map<string, { rev: number, row: { key: string } }>()
          state.set(namespace, rows)
          if (request.method === 'GET') {
            const since = Number(url.searchParams.get('since') ?? 0)
            return json({ rows: [...rows.values()].filter((entry) => entry.rev > since).map((entry) => entry.row), rev })
          }
          let body = ''
          request.on('data', (chunk) => { body += chunk })
          request.on('end', () => {
            const posted = (JSON.parse(body) as { rows: { key: string }[] }).rows
            for (const row of posted) rows.set(row.key, { rev: ++rev, row })
            json({ accepted: posted.map((row) => row.key), rev })
          })
          return
        }
        const agent = url.pathname.match(/^\/api\/agents\/([^/]+)(\/entries)?$/)
        if (agent && agents.includes(agent[1])) {
          if (agent[2]) return json({ sessionId: `session-${agent[1]}`, window: { mode: 'tail', from: 0, limit: 50 }, entries: [], nextOffset: 0 })
          return json({ name: agent[1], tool: 'claude', herdr_status: 'idle', bus_status: 'listening', gap: '', pane: null, launch_context: {} })
        }
        json({ error: 'not found', detail: url.pathname }, 404)
      })
    },
  }
}

test('⌘K, a space name and Enter land the next keystrokes in that space\'s composer', { timeout: 120_000 }, async (context) => {
  const server = await createServer({
    root: new URL('..', import.meta.url).pathname,
    logLevel: 'silent',
    plugins: [fixtureAPI()],
    server: { host: '127.0.0.1', port: 0 },
  })
  await server.listen()
  const url = server.resolvedUrls?.local[0]
  assert.ok(url, 'Vite must expose the browser-test URL')
  const session = `herder-quick-open-focus-${process.pid}`
  const browser = async (args: string[]) => {
    const { stdout } = await execFileAsync('agent-browser', ['--session', session, ...args], { encoding: 'utf8' })
    return stdout.trim()
  }
  const evaluate = async (expression: string) => JSON.parse(await browser(['eval', '-b', Buffer.from(expression).toString('base64')]))
  const waitFor = (expression: string) => browser(['wait', '--fn', expression])
  context.after(async () => {
    try { await browser(['close']) } finally { await server.close() }
  })
  const composer = (agent: string) => `document.querySelector('.dv-active-group textarea[data-composer][aria-label="Message ${agent}"]')`
  const activeComposer = `document.activeElement?.matches?.('textarea[data-composer]') ? document.activeElement.getAttribute('aria-label') : null`
  const selectedRow = `document.querySelector('.quick-open [aria-selected="true"]')?.textContent ?? null`
  const openQuickOpen = async () => {
    await evaluate('document.activeElement?.blur(), true')
    await browser(['press', 'Control+k'])
    await waitFor(`document.activeElement?.closest('.quick-open') !== null`)
  }
  // Opens ⌘K on a non-editable target, types the query and waits for that row to be the Enter target.
  const quickOpen = async (query: string, rowLabel = query) => {
    await openQuickOpen()
    await browser(['keyboard', 'type', query])
    await waitFor(`${selectedRow} === ${JSON.stringify(rowLabel)}`)
  }
  const spaceNames = `JSON.stringify([...document.querySelectorAll('.quick-open-section[aria-label="Spaces"] [role="option"]')].map((row) => row.textContent))`

  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1280', '800'])
  // A cold Vite can re-optimise dependencies and reload mid-boot under a busy suite; one reload covers it.
  await waitFor(`Boolean(${composer('alpha')})`).catch(async () => {
    await browser(['reload'])
    await waitFor(`Boolean(${composer('alpha')})`)
  })

  // The first space's name as ⌘K lists it.
  await openQuickOpen()
  const [first] = JSON.parse(await evaluate(spaceNames)) as string[]
  assert.ok(first, '⌘K must list the first space')

  // A second space, beta, created by clicking its ⌘K row, holding bravo.
  await browser(['keyboard', 'type', 'beta'])
  await waitFor(`[...document.querySelectorAll('.quick-open [role="option"]')].some((row) => row.textContent === 'Create space “beta”')`)
  await evaluate(`[...document.querySelectorAll('.quick-open [role="option"]')].find((row) => row.textContent === 'Create space “beta”').click(), true`)
  await waitFor(`!document.querySelector('.quick-open') && !${composer('alpha')}`)
  await quickOpen('bravo')
  await browser(['press', 'Enter'])
  await waitFor(`Boolean(${composer('bravo')})`)
  await openQuickOpen()
  assert.deepEqual(new Set(JSON.parse(await evaluate(spaceNames))), new Set([first, 'beta']))
  await browser(['press', 'Escape'])
  await waitFor(`!document.querySelector('.quick-open')`)

  await quickOpen(first)
  await browser(['press', 'Enter'])
  // Typed straight after Enter: no wait for the switch, the composer or the palette closing.
  await browser(['keyboard', 'type', 'hi alpha'])
  await waitFor(`${composer('alpha')}?.value === 'hi alpha'`)
  assert.equal(await evaluate(activeComposer), 'Message alpha', 'the palette closing must not take focus back')
  assert.equal(await evaluate(`${composer('alpha')}.selectionStart`), 'hi alpha'.length)

  // Back to beta: the caret goes after bravo's draft.
  await evaluate(`(() => { const field = ${composer('alpha')}; field.blur(); return true })()`)
  await quickOpen('beta')
  await browser(['press', 'Enter'])
  await browser(['keyboard', 'type', 'hey'])
  await waitFor(`${composer('bravo')}?.value === 'hey'`)
  assert.equal(await evaluate(activeComposer), 'Message bravo')

  // ⌘K onto the space already showing refocuses its composer, after the draft.
  await quickOpen('beta')
  await browser(['press', 'Enter'])
  await browser(['keyboard', 'type', ' bravo'])
  await waitFor(`${composer('bravo')}?.value === 'hey bravo'`)
  assert.equal(await evaluate(activeComposer), 'Message bravo')

  // A click on a space row does the same as Enter.
  await quickOpen(first)
  await evaluate(`document.querySelector('.quick-open [aria-selected="true"]').click(), true`)
  await browser(['keyboard', 'type', '!'])
  await waitFor(`${composer('alpha')}?.value === 'hi alpha!'`)
  assert.equal(await evaluate(activeComposer), 'Message alpha')

  // "/" from a non-editable target focuses the composer and types nothing; in the composer it is a slash.
  await evaluate('document.activeElement?.blur(), true')
  await browser(['press', '/'])
  await waitFor(`document.activeElement === ${composer('alpha')}`)
  assert.equal(await evaluate(`${composer('alpha')}.value`), 'hi alpha!')
  await browser(['keyboard', 'type', '/'])
  assert.equal(await evaluate(`${composer('alpha')}.value`), 'hi alpha!/')
  // ...and in the ⌘K box too.
  await evaluate('document.activeElement?.blur(), true')
  await browser(['press', 'Control+k'])
  await waitFor(`document.activeElement?.closest('.quick-open') !== null`)
  await browser(['keyboard', 'type', '/'])
  assert.equal(await evaluate(`document.querySelector('.quick-open input').value`), '/')
  await browser(['press', 'Escape'])
})
