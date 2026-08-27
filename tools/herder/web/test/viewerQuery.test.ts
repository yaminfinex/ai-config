import assert from 'node:assert/strict'
import test from 'node:test'
import { QueryClient } from '@tanstack/react-query'
import { entriesQueryOptions, viewerQueryOptions } from '../src/api/queries.ts'

test('viewer identity retries transient load failures', async () => {
  let attempts = 0
  const fetcher = (async () => {
    attempts += 1
    if (attempts === 1) return new Response(JSON.stringify({ error: 'substrate unreachable', detail: 'temporary failure' }), { status: 502 })
    return new Response(JSON.stringify({ viewer: 'web-owner' }), { status: 200 })
  }) as typeof fetch
  const queryClient = new QueryClient()

  assert.deepEqual(await queryClient.fetchQuery(viewerQueryOptions(fetcher, () => 0)), { viewer: 'web-owner' })
  assert.equal(attempts, 2)
})

test('entry invalidation catches up from nextOffset into the same bounded cache', async () => {
  const calls: string[] = []
  const fetcher = (async (input: RequestInfo | URL) => {
    calls.push(String(input))
    if (calls.length === 1) return new Response(JSON.stringify({
      sessionId: 'session-1', window: { mode: 'tail', from: 0, limit: 500 },
      entries: [{ uuid: 'one', line: 1, byteOffset: 0, kind: 'assistant_text', payload: {} }], nextOffset: 10,
    }), { status: 200 })
    return new Response(JSON.stringify({
      sessionId: 'session-1', window: { mode: 'from', from: 10, limit: 500 },
      entries: [{ uuid: 'two', line: 2, byteOffset: 10, kind: 'assistant_text', payload: {} }], nextOffset: 20,
    }), { status: 200 })
  }) as typeof fetch
  const queryClient = new QueryClient()
  const options = entriesQueryOptions(queryClient, 'vile', fetcher)

  await queryClient.fetchQuery(options)
  await queryClient.invalidateQueries({ queryKey: options.queryKey, exact: true })
  const page = await queryClient.fetchQuery(options)
  assert.deepEqual(page.entries?.map((entry) => entry.uuid), ['one', 'two'])
  assert.deepEqual(calls, ['/api/agents/vile/entries?limit=500', '/api/agents/vile/entries?limit=500&from=10&sessionId=session-1'])
})
