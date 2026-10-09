import assert from 'node:assert/strict'
import test from 'node:test'
import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { deferFleetSubscription, eventStreamURL, fleetTargetsKey, pruneUnsubscribedTranscripts, recordBuildIdentity, streamAlerts, subscribeToFleet, subscriptionUpdateURL, unsubscribedScreenPaneIDs, withoutUnsubscribedTranscripts, type EventSourceLike, type StreamState } from '../src/stream/useFleetStream.ts'
import { queryKeys } from '../src/api/client.ts'
import { entriesQueryOptions } from '../src/api/queries.ts'
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

test('a subscription update names the live stream and the same targets a connection would', () => {
  const targets = { agents: ['zeta', 'alpha'], screens: ['w1:p1'], focusedScreen: 'w1:p1' }
  const url = new URL(subscriptionUpdateURL('stream-1', targets), 'http://fixture')
  assert.equal(url.pathname, '/api/events/subscription')
  assert.equal(url.searchParams.get('stream'), 'stream-1')
  assert.equal(url.searchParams.get('agents'), 'alpha,zeta')
  assert.equal(url.searchParams.get('focused_screen'), 'w1:p1')
  assert.equal(fleetTargetsKey({ agents: ['alpha', 'zeta', 'alpha'] }), fleetTargetsKey({ agents: ['zeta', 'alpha'], screens: [], watches: [] }))
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

// fakeTimers holds every timeout until a test runs it; run(delay) runs the ones scheduled with that delay.
function fakeTimers() {
  const timeouts = new Map<number, { callback: () => void, delay: number }>()
  let timerID = 0
  const timers = {
    setTimeout: ((callback: () => void, delay: number) => { const id = ++timerID; timeouts.set(id, { callback, delay }); return id }) as typeof window.setTimeout,
    clearTimeout: ((id: number) => timeouts.delete(id)) as typeof window.clearTimeout,
    setInterval: (() => 99) as typeof window.setInterval,
    clearInterval: (() => undefined) as typeof window.clearInterval,
  }
  const run = (delay: number) => {
    for (const [id, entry] of [...timeouts]) {
      if (entry.delay !== delay) continue
      timeouts.delete(id)
      entry.callback()
    }
  }
  return { timers, timeouts, run }
}

const realTimers = {
  setTimeout: globalThis.setTimeout as typeof window.setTimeout,
  clearTimeout: globalThis.clearTimeout as typeof window.clearTimeout,
  setInterval: globalThis.setInterval as typeof window.setInterval,
  clearInterval: globalThis.clearInterval as typeof window.clearInterval,
}

const settled = () => new Promise((resolve) => setTimeout(resolve, 0))
// reads counts a query's fetched results: a transcript refresh refetches at once, shown or not.
const reads = (queryClient: QueryClient, key: readonly unknown[]) => queryClient.getQueryState(key)?.dataUpdateCount ?? 0
const transcriptDebounce = 25
const subscriptionSettle = 100

// countedEntries serves a transcript whose first read is the tail and every later one a delta.
function countedEntries(queryClient: QueryClient, name: string) {
  const calls: string[] = []
  const fetcher = (async (input: RequestInfo | URL) => {
    calls.push(String(input))
    const from = new URL(String(input), 'http://fixture').searchParams.get('from')
    const offset = from === null ? 0 : Number(from)
    return new Response(JSON.stringify({
      sessionId: `session-${name}`, window: { mode: from === null ? 'tail' : 'from', from: offset, limit: 500 },
      entries: [{ uuid: `${name}-${calls.length}`, line: calls.length, byteOffset: offset, kind: 'assistant_text', payload: {} }], nextOffset: offset + 10,
    }), { status: 200 })
  }) as typeof fetch
  return { calls, options: entriesQueryOptions(queryClient, name, fetcher) }
}

test('a fleet board refreshes agent rows but fetches no transcript', async () => {
  const queryClient = new QueryClient()
  const vile = countedEntries(queryClient, 'vile')
  const observer = new QueryObserver(queryClient, vile.options)
  const unsubscribe = observer.subscribe(() => undefined)
  await new Promise((resolve) => setTimeout(resolve, 10))
  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  const source = new FakeEventSource()
  const stream = subscribeToFleet(queryClient, { agents: ['vile'] }, { createEventSource: () => source, timers: realTimers })
  source.onopen?.(new Event('open'))
  source.emit('fleet', JSON.stringify({ workspaces: [], unplaced: [] }))
  source.emit('fleet', JSON.stringify({ workspaces: [], unplaced: [] }))
  await new Promise((resolve) => setTimeout(resolve, 60))
  assert.deepEqual(vile.calls, ['/api/agents/vile/entries?limit=500'], 'a board is no reason to read a transcript')
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, false)
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  stream.close()
  unsubscribe()
})

test('an entry for a transcript no panel shows fetches only its delta, in the background', async () => {
  const queryClient = new QueryClient()
  const kumo = countedEntries(queryClient, 'kumo')
  // Read once by a panel that has since gone to another space.
  const observer = new QueryObserver(queryClient, kumo.options)
  const unsubscribe = observer.subscribe(() => undefined)
  await new Promise((resolve) => setTimeout(resolve, 10))
  unsubscribe()
  assert.equal(queryClient.getQueryCache().find({ queryKey: queryKeys.entries('kumo') })?.getObserversCount(), 0)
  const source = new FakeEventSource()
  const stream = subscribeToFleet(queryClient, { agents: ['kumo'] }, { createEventSource: () => source, timers: realTimers })
  source.emit('entry:kumo', '{}')
  source.emit('entry:kumo', '{}')
  await new Promise((resolve) => setTimeout(resolve, 60))
  assert.deepEqual(kumo.calls, ['/api/agents/kumo/entries?limit=500', '/api/agents/kumo/entries?limit=500&from=10&sessionId=session-kumo'])
  assert.deepEqual(queryClient.getQueryData<{ entries: { uuid: string }[] }>(queryKeys.entries('kumo'))?.entries.map((entry) => entry.uuid), ['kumo-1', 'kumo-2'])
  stream.close()
})

test('a rewindow refetches a shown transcript once and one no panel shows from its tail', async () => {
  const queryClient = new QueryClient()
  const kumo = countedEntries(queryClient, 'kumo')
  const observer = new QueryObserver(queryClient, kumo.options)
  const unsubscribe = observer.subscribe(() => undefined)
  await new Promise((resolve) => setTimeout(resolve, 10))
  unsubscribe()
  const vile = countedEntries(queryClient, 'vile')
  const shown = new QueryObserver(queryClient, vile.options)
  const unsubscribeShown = shown.subscribe(() => undefined)
  await new Promise((resolve) => setTimeout(resolve, 10))
  const source = new FakeEventSource()
  const stream = subscribeToFleet(queryClient, { agents: ['kumo', 'vile'] }, { createEventSource: () => source, timers: realTimers })
  source.emit('rewindow', JSON.stringify({ agent: 'kumo' }))
  source.emit('rewindow', JSON.stringify({ agent: 'vile' }))
  await new Promise((resolve) => setTimeout(resolve, 60))
  stream.close()
  unsubscribeShown()
  assert.deepEqual(kumo.calls, ['/api/agents/kumo/entries?limit=500', '/api/agents/kumo/entries?limit=500'], 'a rewindow drops the old window and reads the tail again')
  assert.deepEqual(queryClient.getQueryData<{ entries: { uuid: string }[] }>(queryKeys.entries('kumo'))?.entries.map((entry) => entry.uuid), ['kumo-2'])
  assert.deepEqual(vile.calls, ['/api/agents/vile/entries?limit=500', '/api/agents/vile/entries?limit=500'], 'a shown transcript reads its tail once')
})

test('subscribed catches up every agent it names, shown or not, and a reconnect waits for it', async () => {
  const queryClient = new QueryClient()
  const { timers, run } = fakeTimers()
  const sources: FakeEventSource[] = []
  for (const name of ['vile', 'kumo']) {
    await queryClient.fetchQuery({ queryKey: queryKeys.entries(name), queryFn: async () => ({ entries: [] }) })
    await queryClient.fetchQuery({ queryKey: queryKeys.agent(name), queryFn: async () => ({ name }) })
  }
  await queryClient.fetchQuery({ queryKey: queryKeys.file('/repo', 'README.md'), queryFn: async () => ({ content: 'old' }) })
  const stream = subscribeToFleet(queryClient, { agents: ['vile', 'kumo'], watches: [{ kind: 'file', root: '/repo', path: 'README.md' }] }, {
    createEventSource: () => { const source = new FakeEventSource(); sources.push(source); return source },
    timers,
  })
  const invalidated = (key: readonly unknown[]) => queryClient.getQueryState(key)?.isInvalidated
  const vile = () => reads(queryClient, queryKeys.entries('vile'))
  const kumo = () => reads(queryClient, queryKeys.entries('kumo'))
  sources[0].onopen?.(new Event('open'))
  sources[0].emit('fleet', JSON.stringify({ workspaces: [], unplaced: [] }))
  run(transcriptDebounce)
  await settled()
  assert.deepEqual([vile(), kumo()], [1, 1], 'nothing catches up before the server holds the offsets')
  assert.equal(invalidated(queryKeys.file('/repo', 'README.md')), false, 'a first open has nothing to catch up')
  sources[0].emit('subscribed', JSON.stringify({ agents: ['vile', 'kumo'] }))
  run(transcriptDebounce)
  await settled()
  assert.deepEqual([vile(), kumo()], [2, 2], 'both transcripts catch up, neither one shown')

  sources[0].onerror?.(new Event('error'))
  run(500)
  assert.equal(sources.length, 2)
  sources[1].onopen?.(new Event('open'))
  await settled()
  assert.equal(invalidated(queryKeys.file('/repo', 'README.md')), true, 'a reconnect catches up file watches on open')
  assert.deepEqual([vile(), kumo()], [2, 2], 'a reconnect waits for the server to hold the offsets')
  sources[1].emit('subscribed', JSON.stringify({ agents: ['vile', 'kumo'] }))
  run(transcriptDebounce)
  await settled()
  assert.deepEqual([vile(), kumo()], [3, 3], 'a reconnect catches up every subscribed transcript')
  stream.close()
})

test('a subscription change retargets the live stream once it settles, and reconnects only when the stream refuses', async () => {
  const queryClient = new QueryClient()
  const { timers, run } = fakeTimers()
  const sources: FakeEventSource[] = []
  const urls: string[] = []
  const posts: string[] = []
  let accept = true
  const stream = subscribeToFleet(queryClient, { agents: ['vile'] }, {
    createEventSource: (url) => { urls.push(url); const source = new FakeEventSource(); sources.push(source); return source },
    postSubscription: async (url) => { posts.push(url); return accept },
    timers,
  })
  sources[0].onopen?.(new Event('open'))
  sources[0].emit('hello', JSON.stringify({ buildIdentity: 'source:a', stream: 'stream-1' }))
  assert.deepEqual(posts, [], 'an unchanged subscription posts nothing')

  // A switch passes through intermediate docks; only the settled one is sent.
  stream.update({ agents: ['vile', 'mavu'] })
  stream.update({ agents: ['vile', 'kumo'] })
  assert.deepEqual(posts, [])
  run(subscriptionSettle)
  await settled()
  assert.deepEqual(posts, [subscriptionUpdateURL('stream-1', { agents: ['kumo', 'vile'] })])
  assert.equal(sources.length, 1, 'a switch opens no event stream')
  await queryClient.fetchQuery({ queryKey: queryKeys.entries('kumo'), queryFn: async () => ({ entries: [] }) })
  sources[0].emit('entry:kumo', '{}')
  run(transcriptDebounce)
  await settled()
  assert.equal(reads(queryClient, queryKeys.entries('kumo')), 2, 'the added agent is heard on the same connection')

  accept = false
  stream.update({ agents: ['kumo'] })
  run(subscriptionSettle)
  await settled()
  assert.equal(posts.length, 2)
  assert.equal(sources.length, 2, 'a refused update reconnects')
  assert.equal(sources[0].closed, true)
  assert.equal(urls[1], eventStreamURL(['kumo']))
  stream.close()
})

test('the transcript cache keeps what a space holds or a panel shows, and drops the rest', async () => {
  assert.equal(entriesQueryOptions(new QueryClient(), 'vile').gcTime, Infinity, 'a hidden transcript stays warm for the switch back')
  const queryClient = new QueryClient()
  for (const name of ['vile', 'kumo', 'mavu', 'dore']) await queryClient.fetchQuery({ queryKey: queryKeys.entries(name), queryFn: async () => ({ entries: [] }), gcTime: Infinity })
  const shown = new QueryObserver(queryClient, { queryKey: queryKeys.entries('mavu'), queryFn: async () => ({ entries: [] }), staleTime: Infinity })
  const unsubscribe = shown.subscribe(() => undefined)
  pruneUnsubscribedTranscripts(queryClient, new Set(['vile', 'kumo']))
  const cached = queryClient.getQueryCache().findAll({ queryKey: ['entries'] }).map((query) => query.queryKey[1]).sort()
  assert.deepEqual(cached, ['kumo', 'mavu', 'vile'])
  unsubscribe()

  const { timers, run } = fakeTimers()
  const source = new FakeEventSource()
  const stream = subscribeToFleet(queryClient, { agents: ['vile', 'kumo'] }, { createEventSource: () => source, postSubscription: async () => true, timers })
  stream.update({ agents: ['vile'] })
  assert.equal(queryClient.getQueryData(queryKeys.entries('kumo')) !== undefined, true, 'nothing is dropped mid-switch')
  run(subscriptionSettle)
  const kept = queryClient.getQueryCache().findAll({ queryKey: ['entries'] }).map((query) => query.queryKey[1]).sort()
  assert.deepEqual(kept, ['vile'])
  source.emit('entry:kumo', '{}')
  run(transcriptDebounce)
  await settled()
  assert.equal(queryClient.getQueryCache().find({ queryKey: queryKeys.entries('kumo') }), undefined, 'a dropped agent fetches nothing')
  stream.close()
})

test('multiplexed frames update and invalidate the shared query cache', async () => {
  const queryClient = new QueryClient()
  const sources: FakeEventSource[] = []
  const { timers, run } = fakeTimers()
  await queryClient.fetchQuery({ queryKey: queryKeys.entries('vile'), queryFn: async () => ({ sessionId: 's', window: { mode: 'tail', from: 0, limit: 500 } }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.file('/repo', 'README.md'), queryFn: async () => ({ content: 'old' }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.fileTree('/repo', 'docs'), queryFn: async () => ({ entries: [] }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.backlog('/repo', 'docs'), queryFn: async () => ({ tasks: [] }) })
  queryClient.setQueryData(queryKeys.screen('w1:p1'), { pane_id: 'w1:p1', status: 'available', text: 'stable previous frame', truncated: false })
  queryClient.setQueryData<StreamState>(queryKeys.stream, { problems: {}, substrateProof: { herdr: true, hcom: true }, lastEvent: null, loadedBuild: 'source:previous', serverUpdated: false })
  const stream = subscribeToFleet(queryClient, { agents: ['vile', 'vile'], screens: ['w1:p1'], watches: [
    { kind: 'file', root: '/repo', path: 'README.md' },
    { kind: 'folder', root: '/repo', path: 'docs' },
  ] }, {
    createEventSource: () => {
      const source = new FakeEventSource()
      sources.push(source)
      return source
    },
    timers,
  })

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
  await settled()
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, false, 'a board leaves transcripts alone')
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  assert.equal(reads(queryClient, queryKeys.entries('vile')), 1)
  sources[0].emit('entry:vile')
  run(transcriptDebounce)
  await settled()
  assert.equal(reads(queryClient, queryKeys.entries('vile')), 2)
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)

  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  sources[0].emit('message', JSON.stringify({ id: 731, from: 'web-owner', to: ['vile'], text: 'operator question' }))
  await settled()
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  // A bus message to an open agent refreshes its transcript immediately,
  // independently of the session-file watcher.
  assert.equal(reads(queryClient, queryKeys.entries('vile')), 3)
  sources[0].emit('screen:w1:p1', JSON.stringify({ pane_id: 'w1:p1', status: 'available', text: 'real shell', truncated: false }))
  assert.deepEqual(queryClient.getQueryData(queryKeys.screen('w1:p1')), { pane_id: 'w1:p1', status: 'available', text: 'real shell', truncated: false })
  sources[0].emit('screen:w1:p1', JSON.stringify({ pane_id: 'w1:p1', status: 'unavailable', text: '', truncated: false, detail: 'pane closed' }))
  assert.equal(queryClient.getQueryData<{ text: string }>(queryKeys.screen('w1:p1'))?.text, '')
  sources[0].emit('file-change', JSON.stringify({ kind: 'file', root: '/repo', path: 'README.md' }))
  sources[0].emit('file-change', JSON.stringify({ kind: 'folder', root: '/repo', path: 'docs' }))
  await settled()
  assert.equal(queryClient.getQueryState(queryKeys.file('/repo', 'README.md'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.fileTree('/repo', 'docs'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.backlog('/repo', 'docs'))?.isInvalidated, true)
  stream.close()
  assert.equal(sources[0].closed, true)
})

test('state changes dispatch from the existing EventSource without opening another socket', () => {
  const queryClient = new QueryClient()
  const sources: FakeEventSource[] = []
  const changes: Array<[string, number]> = []
  const stream = subscribeToFleet(queryClient, { agents: [] }, {
    createEventSource: () => {
      const source = new FakeEventSource()
      sources.push(source)
      return source
    },
    timers: fakeTimers().timers,
    onStateChanged: (namespace, rev) => changes.push([namespace, rev]),
  })
  assert.equal(sources.length, 1)
  sources[0].emit('state-changed', JSON.stringify({ namespace: 'spaces', rev: 9 }))
  assert.deepEqual(changes, [['spaces', 9]])
  assert.equal(sources.length, 1)
  stream.close()
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
  await settled()
  assert.equal(cursorFetches, 1)

  const stream = subscribeToFleet(queryClient, { agents: ['vile'] }, { createEventSource: () => source, timers: realTimers })
  source.emit('entry:vile', JSON.stringify({ uuid: 'one' }))
  source.emit('entry:vile', JSON.stringify({ uuid: 'two' }))
  source.emit('entry:vile', JSON.stringify({ uuid: 'three' }))
  await new Promise((resolve) => setTimeout(resolve, 50))

  assert.equal(cursorFetches, 2)
  stream.close()
  unsubscribeObserver()
})

test('an own-send marker suppresses only the duplicate message invalidation', async () => {
  const queryClient = new QueryClient()
  const source = new FakeEventSource()
  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })
  await queryClient.fetchQuery({ queryKey: queryKeys.entries('vile'), queryFn: async () => ({ entries: [] }) })
  const token = beginSendRefresh(queryClient, 'vile')
  const stream = subscribeToFleet(queryClient, { agents: ['vile'] }, { createEventSource: () => source, timers: realTimers })

  source.onopen?.(new Event('open'))
  await settled()
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, false)
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, false)
  source.emit('fleet', JSON.stringify({ workspaces: [], unplaced: [] }))
  await settled()
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(queryKeys.entries('vile'))?.isInvalidated, false)
  await queryClient.fetchQuery({ queryKey: queryKeys.agent('vile'), queryFn: async () => ({ name: 'vile' }) })

  source.emit('message', JSON.stringify({ id: 731, from: 'web-owner', to: ['vile'] }))
  await settled()
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, false)
  assert.equal(reads(queryClient, queryKeys.entries('vile')), 1)

  await settleSendRefresh(token, true, () => undefined)
  const settledReads = reads(queryClient, queryKeys.entries('vile'))
  source.emit('message', JSON.stringify({ id: 732, from: 'someone-else', to: ['vile'] }))
  await settled()
  assert.equal(queryClient.getQueryState(queryKeys.agent('vile'))?.isInvalidated, true)
  assert.equal(reads(queryClient, queryKeys.entries('vile')), settledReads + 1)
  stream.close()
})

function graceHarness() {
  // A pane open resubscribes after the first socket already opened, so the stream problem is absent.
  const queryClient = new QueryClient()
  queryClient.setQueryData<StreamState>(queryKeys.stream, { problems: {}, substrateProof: { herdr: false, hcom: false }, serverUpdated: false, lastEvent: null, loadedBuild: null })
  const sources: FakeEventSource[] = []
  const { timers, timeouts } = fakeTimers()
  const stream = subscribeToFleet(queryClient, { agents: ['vile'] }, { createEventSource: () => { const source = new FakeEventSource(); sources.push(source); return source }, timers })
  const stop = () => stream.close()
  const graceTimers = () => [...timeouts.values()].filter((entry) => entry.delay === 150)
  const streamProblem = () => queryClient.getQueryData<StreamState>(queryKeys.stream)?.problems.stream
  return { sources, stop, graceTimers, streamProblem }
}

test('a resubscribe that opens within the grace window never posts the connecting banner', () => {
  const harness = graceHarness()
  assert.equal(harness.streamProblem(), undefined, 'connect must not post the banner synchronously')
  assert.equal(harness.graceTimers().length, 1)
  harness.sources[0].onopen?.(new Event('open'))
  assert.equal(harness.graceTimers().length, 0, 'open cancels the grace timer')
  assert.equal(harness.streamProblem(), undefined)
  harness.stop()
})

test('a slow open posts the connecting banner after the grace and clears it on open', () => {
  const harness = graceHarness()
  harness.graceTimers()[0].callback()
  assert.equal(harness.streamProblem(), 'Connecting to live fleet…')
  harness.sources[0].onopen?.(new Event('open'))
  assert.equal(harness.streamProblem(), undefined)
  harness.stop()
})

test('teardown during the grace cancels the pending banner', () => {
  const harness = graceHarness()
  harness.stop()
  assert.equal(harness.graceTimers().length, 0)
  assert.equal(harness.streamProblem(), undefined)
})

test('an error during the grace replaces the pending banner with the reconnect detail', () => {
  const harness = graceHarness()
  harness.sources[0].onerror?.(new Event('error'))
  assert.equal(harness.streamProblem(), 'Live stream disconnected; reconnecting…')
  assert.equal(harness.graceTimers().length, 0, 'the grace timer must not overwrite the reconnect detail')
  harness.stop()
})
