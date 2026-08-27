import assert from 'node:assert/strict'
import test from 'node:test'
import { QueryClient } from '@tanstack/react-query'
import { eventStreamURL, recordBuildIdentity, subscribeToFleet, type EventSourceLike, type StreamState } from '../src/stream/useFleetStream.ts'
import { queryKeys } from '../src/api/client.ts'

class FakeEventSource implements EventSourceLike {
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  listeners = new Map<string, Array<(event: { data: string }) => void>>()
  closed = false
  addEventListener(type: string, listener: (event: { data: string }) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener])
  }
  emit(type: string, data = '') {
    this.listeners.get(type)?.forEach((listener) => listener({ data }))
  }
  close() { this.closed = true }
}

test('one stream URL de-duplicates and sorts every open agent', () => {
  assert.equal(eventStreamURL(['zeta', 'alpha', 'zeta']), '/api/events?agents=alpha%2Czeta')
  assert.equal(eventStreamURL([]), '/api/events')
})

test('a changed reconnect build persistently requests a manual refresh', () => {
  const initial: StreamState = { problems: {}, messages: 0, lastEvent: null, loadedBuild: null, serverUpdated: false }
  const loaded = recordBuildIdentity(initial, 'source:build-a')
  assert.equal(loaded.loadedBuild, 'source:build-a')
  assert.equal(loaded.serverUpdated, false)
  assert.equal(recordBuildIdentity(loaded, 'source:build-a').serverUpdated, false)
  const changed = recordBuildIdentity(loaded, 'source:build-b')
  assert.equal(changed.serverUpdated, true)
  assert.equal(recordBuildIdentity(changed, 'source:build-a').serverUpdated, true)
})

test('multiplexed frames update and invalidate the shared query cache', async () => {
  const queryClient = new QueryClient()
  const sources: FakeEventSource[] = []
  const timers = {
    setTimeout: (() => 1) as typeof window.setTimeout,
    clearTimeout: (() => undefined) as typeof window.clearTimeout,
    setInterval: (() => 2) as typeof window.setInterval,
    clearInterval: (() => undefined) as typeof window.clearInterval,
  }
  await queryClient.fetchQuery({ queryKey: queryKeys.entries('vile'), queryFn: async () => ({ sessionId: 's', window: { mode: 'tail', from: 0, limit: 500 } }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  const stop = subscribeToFleet(queryClient, ['vile', 'vile'], () => {
    const source = new FakeEventSource()
    sources.push(source)
    return source
  }, timers)

  assert.equal(sources.length, 1)
  sources[0].emit('hello', JSON.stringify({ buildIdentity: 'source:fixture' }))
  assert.equal(queryClient.getQueryData<{ loadedBuild: string }>(queryKeys.stream)?.loadedBuild, 'source:fixture')
  sources[0].emit('fleet', JSON.stringify({ workspaces: [], unplaced: [] }))
  assert.deepEqual(queryClient.getQueryData(queryKeys.fleet), { workspaces: [], unplaced: [] })
  sources[0].emit('entry:vile')
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  stop()
  assert.equal(sources[0].closed, true)
})
