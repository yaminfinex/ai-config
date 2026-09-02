import assert from 'node:assert/strict'
import test from 'node:test'
import { getAgent, getBacklog, getEntries, getFile, getFileTree, getGitDiff, getGitFile, getGitLog, getGitStatus, lifecycleProblem, resolveFiles, sendMessage, sendPaneInput, spawnAgent, viewerReadOnlyMessage } from '../src/api/client.ts'

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
  await resolveFiles('../README.md', { root: '/repo with space', path: 'docs/guide.md' }, fetcher)
  await resolveFiles('README.md', undefined, fetcher)
  await getFile('/repo with space', 'src/App.tsx', fetcher)
  await getFileTree('/repo with space', 'src/components', fetcher)
  await getBacklog('/repo with space', 'backlog', fetcher)
  assert.deepEqual(calls, [
    '/api/resolve?q=src%2FApp.tsx%3A14&agent=agent+one',
    '/api/resolve?q=..%2FREADME.md&root=%2Frepo+with+space&path=docs%2Fguide.md',
    '/api/resolve?q=README.md',
    '/api/files?root=%2Frepo+with+space&path=src%2FApp.tsx',
    '/api/files/tree?root=%2Frepo+with+space&path=src%2Fcomponents',
    '/api/backlog?root=%2Frepo+with+space&path=backlog',
  ])
})

test('git reads encode roots, paths, mutable bases, cursors, and immutable shas', async () => {
  const calls: string[] = []
  const fetcher = (async (input: RequestInfo | URL) => {
    calls.push(String(input))
    return jsonResponse({})
  }) as typeof fetch
  await getGitStatus('/repo with space', fetcher)
  await getGitStatus('/repo with space', fetcher, undefined, 'branch')
  await getGitDiff('/repo with space', 'src/App.tsx', 'branch', fetcher)
  await getGitDiff('/repo with space', 'old App.tsx', 'commit', fetcher, undefined, '731'.repeat(13) + '7')
  await getGitLog('/repo with space', 'src/App.tsx', 'opaque+/=', fetcher)
  await getGitFile('/repo with space', 'old App.tsx', '731'.repeat(13) + '7', fetcher)
  assert.deepEqual(calls, [
    '/api/git/status?root=%2Frepo+with+space',
    '/api/git/status?root=%2Frepo+with+space&base=branch',
    '/api/git/diff?root=%2Frepo+with+space&path=src%2FApp.tsx&base=branch',
    `/api/git/diff?root=%2Frepo+with+space&path=old+App.tsx&base=commit&sha=${'731'.repeat(13)}7`,
    '/api/git/log?root=%2Frepo+with+space&path=src%2FApp.tsx&cursor=opaque%2B%2F%3D',
    `/api/git/file?root=%2Frepo+with+space&path=old+App.tsx&sha=${'731'.repeat(13)}7`,
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
  await sendPaneInput('w1:p/1', { text: '\x03\x1b[A' }, fetcher)
  await sendPaneInput('w1:p/1', { keys: ['ctrl+c', 'up'] }, fetcher)
  await spawnAgent({ tool: 'codex', model: 'gpt-5.4-mini', tag: 'impl', repo: '/repo root', branch: 'feature/web' }, fetcher)
  assert.deepEqual(requests.map(({ path, init }) => [path, init?.method, init?.body]), [
    ['/api/agents/vile/message', 'POST', JSON.stringify({ text: 'hello' })],
    ['/api/panes/w1%3Ap%2F1/input', 'POST', JSON.stringify({ text: '\x03\x1b[A' })],
    ['/api/panes/w1%3Ap%2F1/input', 'POST', JSON.stringify({ keys: ['ctrl+c', 'up'] })],
    ['/api/spawn', 'POST', JSON.stringify({ tool: 'codex', model: 'gpt-5.4-mini', tag: 'impl', repo: '/repo root', branch: 'feature/web' })],
  ])
})

test('refusals preserve semantic status for lifecycle presentation', async () => {
  const fetcher = (async () => jsonResponse({ error: 'attribution required', detail: 'peer unknown' }, { status: 409 })) as typeof fetch
  await assert.rejects(
    spawnAgent({ tool: 'claude', model: 'opus', tag: 'impl', repo: '' }, fetcher),
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
