import assert from 'node:assert/strict'
import test from 'node:test'
import { getAgent, getEntries, getFile, lifecycleProblem, resolveFiles, sendMessage, spawnAgent, viewerReadOnlyMessage } from '../src/api/client.ts'

function jsonResponse(body: unknown, init?: ResponseInit) {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' }, ...init })
}

test('typed reads encode agent names and transcript cursors', async () => {
  const calls: string[] = []
  const fetcher = (async (input: RequestInfo | URL) => {
    calls.push(String(input))
    return jsonResponse(calls.length === 1 ? { name: 'a/b' } : { sessionId: 's', window: { mode: 'from', from: 7, limit: 500 } })
  }) as typeof fetch

  await getAgent('a/b', fetcher)
  await getEntries('a/b', { from: 7, limit: 500, sessionId: 'session one' }, fetcher)
  assert.deepEqual(calls, [
    '/api/agents/a%2Fb',
    '/api/agents/a%2Fb/entries?limit=500&from=7&sessionId=session+one',
  ])
})

test('file reads encode opaque roots, paths, queries, and optional agent context', async () => {
  const calls: string[] = []
  const fetcher = (async (input: RequestInfo | URL) => {
    calls.push(String(input))
    return jsonResponse({})
  }) as typeof fetch
  await resolveFiles('src/App.tsx:14', 'agent one', fetcher)
  await resolveFiles('README.md', undefined, fetcher)
  await getFile('/repo with space', 'src/App.tsx', fetcher)
  assert.deepEqual(calls, [
    '/api/resolve?q=src%2FApp.tsx%3A14&agent=agent+one',
    '/api/resolve?q=README.md',
    '/api/files?root=%2Frepo+with+space&path=src%2FApp.tsx',
  ])
})

test('mutations use pinned JSON request shapes', async () => {
  const requests: Array<{ path: string, init?: RequestInit }> = []
  const fetcher = (async (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push({ path: String(input), init })
    return jsonResponse(String(input).endsWith('/message')
      ? { sent: true, to: 'vile', from: 'web-owner' }
      : { name: 'new-agent', pane: '' })
  }) as typeof fetch

  await sendMessage('vile', 'hello', fetcher)
  await spawnAgent({ from_pane: 'w1:p1', shape: 'pane', tool: 'codex', tag: 'new', prompt: 'work' }, fetcher)
  assert.deepEqual(requests.map(({ path, init }) => [path, init?.method, init?.body]), [
    ['/api/agents/vile/message', 'POST', JSON.stringify({ text: 'hello' })],
    ['/api/spawn', 'POST', JSON.stringify({ from_pane: 'w1:p1', shape: 'pane', tool: 'codex', tag: 'new', prompt: 'work' })],
  ])
})

test('refusals preserve semantic status for lifecycle presentation', async () => {
  const fetcher = (async () => jsonResponse({ error: 'attribution required', detail: 'peer unknown' }, { status: 409 })) as typeof fetch
  await assert.rejects(
    spawnAgent({ from_pane: 'w1:p1', shape: 'pane', tool: 'codex', tag: 'new', prompt: 'work' }, fetcher),
    (error) => {
      assert.deepEqual(lifecycleProblem(error), { readOnly: 'Connect via Tailscale to continue. peer unknown' })
      return true
    },
  )
})

test('sender collisions explain the collision without connection advice', () => {
  const message = viewerReadOnlyMessage({ error: 'sender refused', detail: 'web-vile already exists' }, 409)
  assert.equal(message, 'Sender collision: this viewer identity maps to a web sender name already reserved or in use. web-vile already exists')
  assert.doesNotMatch(message, /Connect via Tailscale/)
})
