import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import type { ServerResponse } from 'node:http'
import { promisify } from 'node:util'
import type { TestContext } from 'node:test'

import { createServer, type Plugin } from 'vite'

const execFileAsync = promisify(execFile)

type StoredRow = { key: string }
export type FixturePost = { namespace: string, bytes: number, keys: string[], status: number }

// Just enough of the serve API for live idle agents with enabled composers.
// Posted state rows are accepted under a rising rev, and a write body over
// maxWriteBytes is refused with 413 like the serve's decodeWriteBody.
export function fixtureAPI(agents: string[], options: { maxWriteBytes?: number } = {}) {
  const streams = new Set<ServerResponse>()
  const state = new Map<string, Map<string, { rev: number, row: StoredRow }>>()
  const posts: FixturePost[] = []
  let rev = 0
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
  const plugin: Plugin = {
    name: 'herder-fixture-api',
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
          const rows = state.get(namespace) ?? new Map<string, { rev: number, row: StoredRow }>()
          state.set(namespace, rows)
          if (request.method === 'GET') {
            const since = Number(url.searchParams.get('since') ?? 0)
            return json({ rows: [...rows.values()].filter((entry) => entry.rev > since).map((entry) => entry.row), rev })
          }
          let body = ''
          request.on('data', (chunk) => { body += chunk })
          request.on('end', () => {
            const bytes = Buffer.byteLength(body)
            const posted = (JSON.parse(body) as { rows: StoredRow[] }).rows
            const refused = Boolean(options.maxWriteBytes && bytes > options.maxWriteBytes)
            posts.push({ namespace, bytes, keys: posted.map((row) => row.key), status: refused ? 413 : 200 })
            if (refused) return json({ error: 'request too large', detail: `write body exceeds ${options.maxWriteBytes} bytes` }, 413)
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
  return { plugin, state, posts }
}

// startBrowser serves the app on Vite with the given fixture API and drives one agent-browser session.
export async function startBrowser(context: TestContext, name: string, plugin: Plugin) {
  const server = await createServer({
    root: new URL('..', import.meta.url).pathname,
    logLevel: 'silent',
    plugins: [plugin],
    server: { host: '127.0.0.1', port: 0 },
  })
  await server.listen()
  const url = server.resolvedUrls?.local[0]
  assert.ok(url, 'Vite must expose the browser-test URL')
  const session = `herder-${name}-${process.pid}`
  const browser = async (args: string[]) => {
    const { stdout } = await execFileAsync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', maxBuffer: 16 * 1_024 * 1_024 })
    return stdout.trim()
  }
  const evaluate = async (expression: string): Promise<unknown> => JSON.parse(await browser(['eval', '-b', Buffer.from(expression).toString('base64')]))
  const waitFor = (expression: string) => browser(['wait', '--fn', expression])
  context.after(async () => {
    try { await browser(['close']) } finally { await server.close() }
  })
  return { url, browser, evaluate, waitFor }
}
