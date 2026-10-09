import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import type { ServerResponse } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import type { TestContext } from 'node:test'

import { createServer, type Plugin } from 'vite'

const execFileAsync = promisify(execFile)

type StoredRow = { key: string }
export type FixturePost = { namespace: string, bytes: number, keys: string[], status: number }

// Just enough of the serve API for live idle agents with enabled composers.
// Posted state rows are accepted under a rising rev, and a write body over
// maxWriteBytes is refused with 413 like the serve's decodeWriteBody.
// entries gives each agent's transcript; an entries request with `from`
// answers only the entries at or past that byte offset, as a delta. send
// pushes an event to every open stream, and board can be changed before a
// fleet event is sent. Every stream's hello names it; a subscription update
// retargets it. With live set, a stream behaves like the serve's: after
// firstBoardDelayMs it sends the board and announces its agents subscribed,
// and an update announces the agents it adds.
export function fixtureAPI(agents: string[], options: {
  maxWriteBytes?: number
  entries?: (agent: string) => unknown[]
  live?: { firstBoardDelayMs?: number }
} = {}) {
  const streams = new Set<ServerResponse>()
  const streamIDs = new Map<string, { response: ServerResponse, agents: string[] }>()
  const state = new Map<string, Map<string, { rev: number, row: StoredRow }>>()
  const posts: FixturePost[] = []
  const entryRequests: { agent: string, from: number | null, bytes: number }[] = []
  const subscriptionUpdates: string[][] = []
  const held: (() => void)[] = []
  let holding = false
  let opens = 0
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
  const write = (response: ServerResponse, event: string, data: unknown) => response.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`)
  const subscribedAgents = (value: string | null) => (value ?? '').split(',').filter(Boolean)
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
          opens++
          const id = `stream-${opens}`
          const subscribed = subscribedAgents(url.searchParams.get('agents'))
          response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
          response.write(': fixture\n\n')
          write(response, 'hello', { buildIdentity: 'fixture', stream: id })
          streams.add(response)
          streamIDs.set(id, { response, agents: subscribed })
          const firstBoard = options.live && setTimeout(() => {
            write(response, 'fleet', board)
            if (subscribed.length > 0) write(response, 'subscribed', { agents: subscribed })
          }, options.live.firstBoardDelayMs ?? 0)
          request.on('close', () => {
            if (firstBoard) clearTimeout(firstBoard)
            streams.delete(response)
            streamIDs.delete(id)
          })
          return
        }
        if (url.pathname === '/api/events/subscription') {
          const stream = streamIDs.get(url.searchParams.get('stream') ?? '')
          if (request.method !== 'POST' || !stream) return json({ error: 'unknown stream', detail: 'reconnect' }, stream ? 400 : 404)
          const next = subscribedAgents(url.searchParams.get('agents'))
          const added = next.filter((agent) => !stream.agents.includes(agent))
          subscriptionUpdates.push(next)
          stream.agents = next
          if (options.live && added.length > 0) write(stream.response, 'subscribed', { agents: added })
          response.statusCode = 204
          response.end()
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
          if (agent[2]) {
            const from = url.searchParams.has('from') ? Number(url.searchParams.get('from')) : null
            const all = (options.entries?.(agent[1]) ?? []) as { byteOffset?: number }[]
            const entries = from === null ? all : all.filter((entry) => (entry.byteOffset ?? 0) >= from)
            const end = all.reduce((latest, entry) => Math.max(latest, (entry.byteOffset ?? 0) + 1), 0)
            const body = JSON.stringify({ sessionId: `session-${agent[1]}`, window: { mode: 'tail', from: 0, limit: 50 }, entries, nextOffset: end })
            const answer = () => {
              entryRequests.push({ agent: agent[1], from, bytes: Buffer.byteLength(body) })
              response.setHeader('Content-Type', 'application/json')
              response.end(body)
            }
            if (holding) held.push(answer)
            else answer()
            return
          }
          return json({ name: agent[1], tool: 'claude', herdr_status: 'idle', bus_status: 'listening', gap: '', pane: null, launch_context: {} })
        }
        json({ error: 'not found', detail: url.pathname }, 404)
      })
    },
  }
  const send = (event: string, data: unknown) => { for (const stream of streams) write(stream, event, data) }
  // holdEntries(true) leaves entries requests unanswered (and unrecorded)
  // until holdEntries(false), so a page that renders meanwhile proves it
  // did not wait on the network.
  const holdEntries = (on: boolean) => {
    holding = on
    if (!on) held.splice(0).forEach((answer) => answer())
  }
  return { plugin, state, posts, board, send, entryRequests, subscriptionUpdates, holdEntries, opens: () => opens, heldEntries: () => held.length }
}

// A minimal React devtools hook, installed before React loads, that counts
// each component that rendered in a commit: mounted, or updated with work
// performed. Subtrees React bailed out of are skipped.
export const renderCounter = `(() => {
  window.__renders = {}
  window.__commits = 0
  window.__track = false
  const label = (fiber) => {
    const type = fiber.type
    if (typeof type === 'function') return type.displayName || type.name || null
    if (type && typeof type === 'object') { const inner = type.render || type.type; return type.displayName || inner?.displayName || inner?.name || null }
    return null
  }
  const count = (name) => { window.__renders[name] = (window.__renders[name] ?? 0) + 1 }
  const walk = (root) => {
    const stack = root ? [root] : []
    while (stack.length) {
      const fiber = stack.pop()
      const name = [0, 1, 11, 14, 15].includes(fiber.tag) ? label(fiber) : null
      const mounted = fiber.alternate === null
      if (name && mounted) count('mount:' + name)
      else if (name && (fiber.flags & 1)) count(name)
      if (fiber.sibling) stack.push(fiber.sibling)
      if (fiber.child && (mounted || fiber.child !== fiber.alternate.child)) stack.push(fiber.child)
    }
  }
  window.__REACT_DEVTOOLS_GLOBAL_HOOK__ = {
    supportsFiber: true, renderers: new Map(), isDisabled: false,
    inject(renderer) { const id = this.renderers.size + 1; this.renderers.set(id, renderer); return id },
    onCommitFiberRoot(_id, root) { if (!window.__track) return; window.__commits++; walk(root.current.child) },
    onCommitFiberUnmount() {}, onPostCommitFiberRoot() {}, checkDCE() {}, on() {}, off() {}, emit() {}, sub() { return () => {} },
  }
})()`

// viteCacheDir gives a test's Vite server its own dep cache: browser tests run
// in parallel, and a shared node_modules/.vite let one server's re-optimization
// stall another page's module loads. Remove it once the server has closed.
export async function viteCacheDir(name: string) {
  const cacheDir = await mkdtemp(join(tmpdir(), `herder-vite-${name}-`))
  return { cacheDir, remove: () => rm(cacheDir, { recursive: true, force: true }) }
}

// startBrowser serves the app on Vite with the given fixture API and drives one agent-browser session.
export async function startBrowser(context: TestContext, name: string, plugin: Plugin) {
  const cache = await viteCacheDir(name)
  const server = await createServer({
    root: new URL('..', import.meta.url).pathname,
    cacheDir: cache.cacheDir,
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
    try { await browser(['close']) } finally { await server.close(); await cache.remove() }
  })
  return { url, browser, evaluate, waitFor }
}
