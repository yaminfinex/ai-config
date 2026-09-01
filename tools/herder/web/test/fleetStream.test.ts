import assert from 'node:assert/strict'
import test from 'node:test'
import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { deferFleetSubscription, eventStreamURL, recordBuildIdentity, streamAlerts, subscribeToFleet, unsubscribedScreenPaneIDs, withoutUnsubscribedTranscripts, type EventSourceLike, type StreamState } from '../src/stream/useFleetStream.ts'
import { queryKeys } from '../src/api/client.ts'
import { beginSendRefresh, settleSendRefresh } from '../src/sendRefresh.ts'

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
  assert.equal(eventStreamURL(['zeta'], ['w2:p9', 'w1:p1', 'w2:p9']), '/api/events?agents=zeta&screens=w1%3Ap1%2Cw2%3Ap9')
  const watches = [{ kind: 'file' as const, root: '/repo', path: 'README.md' }, { kind: 'folder' as const, root: '/repo', path: 'docs' }]
  const url = new URL(eventStreamURL([], [], watches), 'http://fixture')
  assert.deepEqual(JSON.parse(url.searchParams.get('watches') ?? ''), watches)
  const focused = new URL(eventStreamURL([], ['w1:p1'], watches, 'w1:p1'), 'http://fixture')
  assert.equal(focused.searchParams.get('focused_screen'), 'w1:p1')
  assert.deepEqual(JSON.parse(focused.searchParams.get('watches') ?? ''), watches)
})

test('closing the last screen consumer identifies only stale screen caches', () => {
  assert.deepEqual(unsubscribedScreenPaneIDs(['w1:p1', 'w2:p2', 'w1:p1'], ['w2:p2', 'w3:p3']), ['w1:p1'])
})

test('a changed reconnect build persistently requests a manual refresh', () => {
  const initial: StreamState = { problems: {}, substrateProof: { herdr: false, hcom: false }, lastEvent: null, loadedBuild: null, serverUpdated: false }
  const loaded = recordBuildIdentity(initial, 'source:build-a')
  assert.equal(loaded.loadedBuild, 'source:build-a')
  assert.equal(loaded.serverUpdated, false)
  assert.equal(recordBuildIdentity(loaded, 'source:build-a').serverUpdated, false)
  const changed = recordBuildIdentity(loaded, 'source:build-b')
  assert.equal(changed.serverUpdated, true)
  assert.equal(recordBuildIdentity(changed, 'source:build-a').serverUpdated, true)
})

test('last-event traffic does not notify alert-only stream consumers', async () => {
  const queryClient = new QueryClient()
  const initial: StreamState = { problems: {}, substrateProof: { herdr: true, hcom: true }, lastEvent: 1, loadedBuild: 'source:a', serverUpdated: false }
  queryClient.setQueryData(queryKeys.stream, initial)
  const observer = new QueryObserver(queryClient, {
    queryKey: queryKeys.stream,
    queryFn: async () => initial,
    select: streamAlerts,
    notifyOnChangeProps: ['data'],
  })
  let notifications = 0
  const unsubscribe = observer.subscribe(() => { notifications += 1 })
  const baseline = notifications
  queryClient.setQueryData<StreamState>(queryKeys.stream, (current) => ({ ...(current ?? initial), lastEvent: 2 }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(notifications, baseline)
  queryClient.setQueryData<StreamState>(queryKeys.stream, (current) => ({ ...(current ?? initial), problems: { stream: 'reconnecting' } }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(notifications, baseline + 1)
  unsubscribe()
})

test('closing an agent subscription prunes only that transcript fault', () => {
  assert.deepEqual(withoutUnsubscribedTranscripts({
    stream: 'reconnecting',
    'transcript:retired': 'gone',
    'transcript:still-open': 'unreadable',
  }, ['still-open']), {
    stream: 'reconnecting',
    'transcript:still-open': 'unreadable',
  })
})

test('switch-settling cancels the stale subscription before it opens', () => {
  const scheduled = new Map<number, () => void>()
  let next = 0
  const starts: string[] = []
  const schedule = (callback: () => void) => {
    const id = ++next
    scheduled.set(id, () => { scheduled.delete(id); callback() })
    return id
  }
  const cancel = (id: number) => scheduled.delete(id)
  const stale = deferFleetSubscription(() => { starts.push('stale'); return () => undefined }, schedule, cancel, schedule, cancel)
  stale()
  const settled = deferFleetSubscription(() => { starts.push('settled'); return () => undefined }, schedule, cancel, schedule, cancel)
  for (const callback of [...scheduled.values()]) callback()
  assert.deepEqual(starts, [], 'one frame is reserved for child watch registrations to settle')
  for (const callback of [...scheduled.values()]) callback()
  assert.deepEqual(starts, ['settled'])
  settled()
})

test('first open skips catch-up invalidations but an established reconnect keeps them', async () => {
  const queryClient = new QueryClient()
  const sources: FakeEventSource[] = []
  const timeouts = new Map<number, () => void>()
  let timerID = 0
  const timers = {
    setTimeout: ((callback: () => void) => { const id = ++timerID; timeouts.set(id, callback); return id }) as typeof window.setTimeout,
    clearTimeout: ((id: number) => timeouts.delete(id)) as typeof window.clearTimeout,
    setInterval: (() => 99) as typeof window.setInterval,
    clearInterval: (() => undefined) as typeof window.clearInterval,
  }
  const clean = async () => {
    await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
    await queryClient.fetchQuery({ queryKey: queryKeys.entries('vile'), queryFn: async () => ({ entries: [] }) })
    await queryClient.fetchQuery({ queryKey: queryKeys.file('/repo', 'README.md'), queryFn: async () => ({ content: 'old' }) })
  }
  await clean()
  const stop = subscribeToFleet(queryClient, ['vile'], [], [{ kind: 'file', root: '/repo', path: 'README.md' }], undefined, () => {
    const source = new FakeEventSource(); sources.push(source); return source
  }, timers)
  sources[0].onopen?.(new Event('open'))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, false)
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, false)
  assert.equal(queryClient.getQueryState(queryKeys.file('/repo', 'README.md'))?.isInvalidated, false)

  sources[0].onerror?.(new Event('error'))
  const reconnect = [...timeouts.values()][0]
  assert.ok(reconnect)
  reconnect()
  assert.equal(sources.length, 2)
  sources[1].onopen?.(new Event('open'))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.file('/repo', 'README.md'))?.isInvalidated, true)
  stop()
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
  await queryClient.fetchQuery({ queryKey: queryKeys.file('/repo', 'README.md'), queryFn: async () => ({ content: 'old' }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.fileTree('/repo', 'docs'), queryFn: async () => ({ entries: [] }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.backlog('/repo', 'docs'), queryFn: async () => ({ tasks: [] }) })
  queryClient.setQueryData(queryKeys.screen('w1:p1'), { pane_id: 'w1:p1', status: 'available', text: 'stable previous frame', truncated: false })
  queryClient.setQueryData<StreamState>(queryKeys.stream, { problems: {}, substrateProof: { herdr: true, hcom: true }, lastEvent: null, loadedBuild: 'source:previous', serverUpdated: false })
  const stop = subscribeToFleet(queryClient, ['vile', 'vile'], ['w1:p1'], [
    { kind: 'file', root: '/repo', path: 'README.md' },
    { kind: 'folder', root: '/repo', path: 'docs' },
  ], undefined, () => {
    const source = new FakeEventSource()
    sources.push(source)
    return source
  }, timers)

  assert.equal(sources.length, 1)
  assert.equal(queryClient.getQueryData<{ text: string }>(queryKeys.screen('w1:p1'))?.text, 'stable previous frame')
  assert.deepEqual(queryClient.getQueryData<StreamState>(queryKeys.stream)?.substrateProof, { herdr: false, hcom: false })
  sources[0].onopen?.(new Event('open'))
  assert.equal(queryClient.getQueryData<StreamState>(queryKeys.stream)?.problems.stream, undefined)
  sources[0].emit('hello', JSON.stringify({ buildIdentity: 'source:fixture' }))
  assert.equal(queryClient.getQueryData<StreamState>(queryKeys.stream)?.loadedBuild, 'source:previous')
  assert.equal(queryClient.getQueryData<StreamState>(queryKeys.stream)?.serverUpdated, true)
  sources[0].emit('fleet', JSON.stringify({ workspaces: [], unplaced: [] }))
  assert.deepEqual(queryClient.getQueryData(queryKeys.fleet), { workspaces: [], unplaced: [] })
  assert.deepEqual(queryClient.getQueryData<StreamState>(queryKeys.stream)?.substrateProof, { herdr: true, hcom: false })
  sources[0].emit('substrate', JSON.stringify({ source: 'hcom', status: 'recovered' }))
  assert.deepEqual(queryClient.getQueryData<StreamState>(queryKeys.stream)?.substrateProof, { herdr: true, hcom: true })
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  sources[0].emit('entry:vile')
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)

  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.entries('vile'), queryFn: async () => ({ sessionId: 's', window: { mode: 'tail', from: 0, limit: 500 } }) })
  sources[0].emit('message', JSON.stringify({ id: 731, from: 'web-owner', to: ['vile'], text: 'operator question' }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  // A bus message to an open agent refreshes its transcript immediately,
  // independently of the session-file watcher.
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, true)
  sources[0].emit('screen:w1:p1', JSON.stringify({ pane_id: 'w1:p1', status: 'available', text: 'real shell', truncated: false }))
  assert.deepEqual(queryClient.getQueryData(queryKeys.screen('w1:p1')), { pane_id: 'w1:p1', status: 'available', text: 'real shell', truncated: false })
  sources[0].emit('screen:w1:p1', JSON.stringify({ pane_id: 'w1:p1', status: 'unavailable', text: '', truncated: false, detail: 'pane closed' }))
  assert.equal(queryClient.getQueryData<{ text: string }>(queryKeys.screen('w1:p1'))?.text, '')
  sources[0].emit('file-change', JSON.stringify({ kind: 'file', root: '/repo', path: 'README.md' }))
  sources[0].emit('file-change', JSON.stringify({ kind: 'folder', root: '/repo', path: 'docs' }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.file('/repo', 'README.md'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.fileTree('/repo', 'docs'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.backlog('/repo', 'docs'))?.isInvalidated, true)
  stop()
  assert.equal(sources[0].closed, true)
})

test('one multi-entry SSE burst causes one active transcript cursor fetch', async () => {
  const queryClient = new QueryClient()
  const source = new FakeEventSource()
  let cursorFetches = 0
  const observer = new QueryObserver(queryClient, {
    queryKey: queryKeys.entries('vile'),
    queryFn: async () => {
      cursorFetches++
      return { sessionId: 's', window: { mode: 'from' as const, from: 0, limit: 500 }, entries: [], nextOffset: 0 }
    },
  })
  const unsubscribeObserver = observer.subscribe(() => undefined)
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(cursorFetches, 1)

  const timers = {
    setTimeout: globalThis.setTimeout as typeof window.setTimeout,
    clearTimeout: globalThis.clearTimeout as typeof window.clearTimeout,
    setInterval: globalThis.setInterval as typeof window.setInterval,
    clearInterval: globalThis.clearInterval as typeof window.clearInterval,
  }
  const stop = subscribeToFleet(queryClient, ['vile'], [], [], undefined, () => source, timers)
  source.emit('entry:vile', JSON.stringify({ uuid: 'one' }))
  source.emit('entry:vile', JSON.stringify({ uuid: 'two' }))
  source.emit('entry:vile', JSON.stringify({ uuid: 'three' }))
  await new Promise((resolve) => setTimeout(resolve, 50))

  assert.equal(cursorFetches, 2)
  stop()
  unsubscribeObserver()
})

test('an own-send marker suppresses only the duplicate message invalidation', async () => {
  const queryClient = new QueryClient()
  const source = new FakeEventSource()
  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.entries('vile'), queryFn: async () => ({ entries: [] }) })
  const token = beginSendRefresh(queryClient, 'vile')
  const timers = {
    setTimeout: globalThis.setTimeout as typeof window.setTimeout,
    clearTimeout: globalThis.clearTimeout as typeof window.clearTimeout,
    setInterval: globalThis.setInterval as typeof window.setInterval,
    clearInterval: globalThis.clearInterval as typeof window.clearInterval,
  }
  const stop = subscribeToFleet(queryClient, ['vile'], [], [], undefined, () => source, timers)

  source.onopen?.(new Event('open'))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, false)
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, false)
  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.entries('vile'), queryFn: async () => ({ entries: [] }) })
  source.emit('fleet', JSON.stringify({ workspaces: [], unplaced: [] }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, true)
  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.entries('vile'), queryFn: async () => ({ entries: [] }) })

  source.emit('message', JSON.stringify({ id: 731, from: 'web-owner', to: ['vile'] }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, false)
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, false)

  await settleSendRefresh(token, true, () => undefined)
  source.emit('message', JSON.stringify({ id: 732, from: 'someone-else', to: ['vile'] }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, true)
  stop()
})
