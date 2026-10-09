import { useEffect, useRef } from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { queryKeys } from '../api/client.ts'
import type { Board, ScreenFrame, SubstrateEvent } from '../types'
import type { FileWatchTarget } from './fileWatchRegistry.ts'
import { deferMessageRefresh } from '../sendRefresh.ts'

export type StreamState = {
  problems: Record<string, string>
  substrateProof: { herdr: boolean, hcom: boolean }
  lastEvent: number | null
  loadedBuild: string | null
  serverUpdated: boolean
}

type StreamAlerts = Pick<StreamState, 'problems' | 'serverUpdated'>

export function streamAlerts(stream: StreamState): StreamAlerts {
  return { problems: stream.problems, serverUpdated: stream.serverUpdated }
}

const initialStreamState: StreamState = {
  problems: { stream: 'Connecting to live fleet…' },
  substrateProof: { herdr: false, hcom: false },
  lastEvent: null,
  loadedBuild: null,
  serverUpdated: false,
}

export function recordBuildIdentity(current: StreamState, identity: string): StreamState {
  if (!current.loadedBuild) return { ...current, loadedBuild: identity }
  if (current.loadedBuild !== identity) return { ...current, serverUpdated: true }
  return current
}

type StreamEvent = { data: string }
export type EventSourceLike = {
  onopen: ((event: Event) => void) | null
  onerror: ((event: Event) => void) | null
  addEventListener(type: string, listener: (event: StreamEvent) => void): void
  close(): void
}

type TimerHost = Pick<typeof window, 'setTimeout' | 'clearTimeout' | 'setInterval' | 'clearInterval'>
const transcriptBurstDebounce = 25

function without(problem: Record<string, string>, key: string) {
  const next = { ...problem }
  delete next[key]
  return next
}

export function withoutUnsubscribedTranscripts(problems: Record<string, string>, agentNames: string[]) {
  const subscribed = new Set(agentNames.map((name) => `transcript:${name}`))
  return Object.fromEntries(Object.entries(problems).filter(([source]) => !source.startsWith('transcript:') || subscribed.has(source)))
}

// FleetTargets is what the one event stream follows: transcripts by agent,
// screen frames by pane, file watches, and the focused screen.
export type FleetTargets = {
  agents: string[]
  screens?: string[]
  watches?: FileWatchTarget[]
  focusedScreen?: string
}

function eventQuery({ agents, screens = [], watches = [], focusedScreen }: FleetTargets) {
  const query = new URLSearchParams()
  const agentList = [...new Set(agents)].sort().join(',')
  const screenList = [...new Set(screens)].sort().join(',')
  if (agentList) query.set('agents', agentList)
  if (screenList) query.set('screens', screenList)
  if (watches.length > 0) query.set('watches', JSON.stringify(watches))
  if (focusedScreen) query.set('focused_screen', focusedScreen)
  return query
}

// fleetTargetsKey is one string per distinct subscription; targetsFromKey reads it back.
export function fleetTargetsKey(targets: FleetTargets) {
  return eventQuery(targets).toString()
}

function targetsFromKey(key: string): FleetTargets {
  const query = new URLSearchParams(key)
  const list = (name: string) => (query.get(name) ?? '').split(',').filter(Boolean)
  return { agents: list('agents'), screens: list('screens'), watches: JSON.parse(query.get('watches') ?? '[]') as FileWatchTarget[], focusedScreen: query.get('focused_screen') ?? undefined }
}

export function eventStreamURL(agentNames: string[], screenPaneIDs: string[] = [], fileWatches: FileWatchTarget[] = [], focusedScreenPaneID?: string) {
  const query = eventQuery({ agents: agentNames, screens: screenPaneIDs, watches: fileWatches, focusedScreen: focusedScreenPaneID })
  return query.size ? `/api/events?${query}` : '/api/events'
}

export function subscriptionUpdateURL(stream: string, targets: FleetTargets) {
  const query = eventQuery(targets)
  query.set('stream', stream)
  return `/api/events/subscription?${query}`
}

export function unsubscribedScreenPaneIDs(previous: string[], current: string[]) {
  const subscribed = new Set(current)
  return [...new Set(previous)].filter((paneID) => !subscribed.has(paneID))
}

// pruneUnsubscribedTranscripts drops cached transcripts the stream no longer
// keeps live and no panel shows. Transcripts never expire on their own, so
// the subscription is what bounds the cache.
export function pruneUnsubscribedTranscripts(queryClient: QueryClient, agentNames: ReadonlySet<string>) {
  for (const query of queryClient.getQueryCache().findAll({ queryKey: ['entries'] })) {
    const name = query.queryKey[1]
    if (typeof name === 'string' && !agentNames.has(name) && query.getObserversCount() === 0) queryClient.removeQueries({ queryKey: query.queryKey, exact: true })
  }
}

const connectingBannerGrace = 150
// A space switch rebuilds the dock in steps; a subscription change waits
// for them to settle so a passing subset neither retargets nor prunes.
const subscriptionSettle = 100

export type FleetStream = {
  update(targets: FleetTargets): void
  close(): void
}

type FleetStreamOptions = {
  createEventSource?: (url: string) => EventSourceLike
  // postSubscription asks the live stream to follow new targets; false means it could not, so the stream reconnects.
  postSubscription?: (url: string) => Promise<boolean>
  timers?: TimerHost
  onStateChanged?: (namespace: string, rev: number) => void
}

async function postSubscriptionUpdate(url: string) {
  const response = await fetch(url, { method: 'POST' })
  return response.status === 204
}

// subscribeToFleet keeps one event stream for the page. A changed
// subscription retargets the live connection with a POST; only a stream
// that cannot take it reconnects. Transcripts refresh only on their own
// entry, message and subscribed events, warm or not shown.
export function subscribeToFleet(queryClient: QueryClient, initial: FleetTargets, {
  createEventSource = (url) => new EventSource(url),
  postSubscription = postSubscriptionUpdate,
  timers = window,
  onStateChanged,
}: FleetStreamOptions = {}): FleetStream {
  let active = true
  let events: EventSourceLike | null = null
  let reconnectTimer: number | null = null
  let watchdog: number | null = null
  let connectingTimer: number | null = null
  let settleTimer: number | null = null
  const transcriptRefreshTimers = new Map<string, number>()
  let backoff = 500
  let hasOpened = false
  let lastActivity = Date.now()
  let targets = targetsFromKey(fleetTargetsKey(initial))
  let names = new Set(targets.agents)
  let panes = new Set(targets.screens)
  // following is the subscription the live connection serves; streamID names it for an update.
  let following = ''
  let streamID: string | null = null
  let posting = false
  let listening = new Set<string>()
  const invalidateFileWatch = (fact: FileWatchTarget) => {
    if (fact.kind === 'file') {
      void queryClient.invalidateQueries({ queryKey: queryKeys.file(fact.root, fact.path), exact: true })
      void queryClient.invalidateQueries({ queryKey: queryKeys.fileRaw(fact.root, fact.path), exact: true })
      return
    }
    void queryClient.invalidateQueries({ queryKey: queryKeys.fileTree(fact.root, fact.path), exact: true })
    void queryClient.invalidateQueries({ queryKey: queryKeys.backlog(fact.root, fact.path), exact: true })
  }
  // A transcript refreshes in the background even with no panel showing
  // it, so a space switch renders it without a fetch.
  const refreshTranscript = (name: string) => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true, refetchType: 'all' })
    void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
  }
  const scheduleTranscriptRefresh = (name: string) => {
    if (transcriptRefreshTimers.has(name)) return
    const timer = timers.setTimeout(() => {
      transcriptRefreshTimers.delete(name)
      if (names.has(name)) refreshTranscript(name)
    }, transcriptBurstDebounce)
    transcriptRefreshTimers.set(name, timer)
  }
  const update = (change: (current: StreamState) => StreamState) => {
    queryClient.setQueryData<StreamState>(queryKeys.stream, (current) => change(current ?? initialStreamState))
  }
  update((current) => ({ ...current, problems: withoutUnsubscribedTranscripts(current.problems, targets.agents) }))
  const touch = (visible = true) => {
    lastActivity = Date.now()
    if (visible) update((current) => ({ ...current, lastEvent: lastActivity }))
  }
  const cancelConnecting = () => {
    if (connectingTimer !== null) timers.clearTimeout(connectingTimer)
    connectingTimer = null
  }
  const scheduleReconnect = (detail: string) => {
    if (!active || reconnectTimer !== null) return
    cancelConnecting()
    events?.close()
    events = null
    update((current) => ({ ...current, problems: { ...current.problems, stream: detail } }))
    reconnectTimer = timers.setTimeout(() => {
      reconnectTimer = null
      connect()
    }, backoff)
    backoff = Math.min(backoff * 2, 10_000)
  }
  // listen adds the per-agent and per-pane listeners the connection lacks;
  // a listener left from a dropped target ignores its events.
  const listen = () => {
    const source = events
    if (!source) return
    for (const name of names) {
      if (listening.has(`entry:${name}`)) continue
      listening.add(`entry:${name}`)
      source.addEventListener(`entry:${name}`, () => {
        touch()
        scheduleTranscriptRefresh(name)
      })
    }
    for (const paneID of panes) {
      if (listening.has(`screen:${paneID}`)) continue
      listening.add(`screen:${paneID}`)
      source.addEventListener(`screen:${paneID}`, (event) => {
        touch()
        if (panes.has(paneID)) queryClient.setQueryData<ScreenFrame>(queryKeys.screen(paneID), JSON.parse(event.data) as ScreenFrame)
      })
    }
  }
  // retarget sends the live connection the latest subscription, one update at a time.
  const retarget = () => {
    const connection = events
    if (!active || !connection || !streamID || posting) return
    const wanted = fleetTargetsKey(targets)
    if (wanted === following) return
    posting = true
    void postSubscription(subscriptionUpdateURL(streamID, targets)).catch(() => false).then((taken) => {
      posting = false
      if (!active || events !== connection) return
      if (!taken) {
        cancelConnecting()
        events.close()
        events = null
        connect()
        return
      }
      following = wanted
      retarget()
    })
  }
  const connect = () => {
    if (!active) return
    lastActivity = Date.now()
    update((current) => ({ ...current, substrateProof: { herdr: false, hcom: false } }))
    // Avoid shifting the pane grid during a fast stream resubscribe.
    connectingTimer = timers.setTimeout(() => {
      connectingTimer = null
      update((current) => ({ ...current, problems: { ...current.problems, stream: 'Connecting to live fleet…' } }))
    }, connectingBannerGrace)
    streamID = null
    listening = new Set()
    following = fleetTargetsKey(targets)
    try {
      events = createEventSource(eventStreamURL(targets.agents, targets.screens, targets.watches, targets.focusedScreen))
    } catch {
      scheduleReconnect('Live stream disconnected; reconnecting…')
      return
    }
    const watches = targets.watches ?? []
    events.onopen = () => {
      touch()
      cancelConnecting()
      backoff = 500
      update((current) => ({ ...current, problems: without(current.problems, 'stream') }))
      // Transcripts catch up on the subscribed event, once their offsets are held.
      if (hasOpened) watches.forEach(invalidateFileWatch)
      hasOpened = true
    }
    events.onerror = () => scheduleReconnect('Live stream disconnected; reconnecting…')
    events.addEventListener('hello', (event) => {
      touch()
      const { buildIdentity, stream } = JSON.parse(event.data) as { buildIdentity: string, stream?: string }
      update((current) => recordBuildIdentity(current, buildIdentity))
      streamID = stream ?? null
      if (settleTimer === null) retarget()
    })
    events.addEventListener('ping', () => touch(false))
    events.addEventListener('state-changed', (event) => {
      touch()
      const change = JSON.parse(event.data) as { namespace: string, rev: number }
      onStateChanged?.(change.namespace, change.rev)
    })
    // The server holds these agents' transcript offsets from here: anything
    // written since a cached page was read is fetched now.
    events.addEventListener('subscribed', (event) => {
      touch()
      const { agents } = JSON.parse(event.data) as { agents: string[] }
      agents.forEach(scheduleTranscriptRefresh)
    })
    events.addEventListener('fleet', (event) => {
      touch()
      queryClient.setQueryData<Board>(queryKeys.fleet, JSON.parse(event.data) as Board)
      names.forEach((name) => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
      })
      update((current) => ({
        ...current,
        substrateProof: { ...current.substrateProof, herdr: true },
        problems: without(current.problems, 'fleet'),
      }))
    })
    events.addEventListener('substrate', (event) => {
      touch()
      const state = JSON.parse(event.data) as SubstrateEvent
      update((current) => ({
        ...current,
        substrateProof: state.status === 'recovered' && (state.source === 'herdr' || state.source === 'hcom')
          ? { ...current.substrateProof, [state.source]: true }
          : current.substrateProof,
        problems: state.status === 'recovered'
          ? without(current.problems, state.source)
          : { ...current.problems, [state.source]: state.detail ?? `${state.source} is unreachable` },
      }))
    })
    events.addEventListener('message', (event) => {
      touch()
      const { to } = JSON.parse(event.data) as { to?: string[] }
      to?.filter((name) => names.has(name)).forEach((name) => {
        if (deferMessageRefresh(queryClient, name)) return
        refreshTranscript(name)
      })
    })
    events.addEventListener('rewindow', (event) => {
      touch()
      const { agent } = JSON.parse(event.data) as { agent: string }
      if (!names.has(agent)) return
      // A reset refetches only shown transcripts, and refetchQueries skips a reset one no panel
      // observes (it counts as disabled), so a warm one not shown is fetched directly.
      const warm = queryClient.getQueryCache().find({ queryKey: queryKeys.entries(agent), exact: true })
      void queryClient.resetQueries({ queryKey: queryKeys.entries(agent), exact: true }).then(() => {
        if (warm && warm.getObserversCount() === 0) return warm.fetch().then(() => undefined, () => undefined)
      })
      void queryClient.invalidateQueries({ queryKey: queryKeys.agent(agent), exact: true })
    })
    events.addEventListener('file-change', (event) => {
      touch()
      const fact = JSON.parse(event.data) as FileWatchTarget
      if (fact.kind === 'file' || fact.kind === 'folder') invalidateFileWatch(fact)
    })
    listen()
  }

  connect()
  watchdog = timers.setInterval(() => {
    if (Date.now() - lastActivity > 45_000) scheduleReconnect('Live stream timed out; reconnecting…')
  }, 5_000)

  return {
    update(next) {
      if (!active) return
      targets = targetsFromKey(fleetTargetsKey(next))
      names = new Set(targets.agents)
      panes = new Set(targets.screens)
      listen()
      if (settleTimer !== null) timers.clearTimeout(settleTimer)
      settleTimer = timers.setTimeout(() => {
        settleTimer = null
        update((current) => ({ ...current, problems: withoutUnsubscribedTranscripts(current.problems, targets.agents) }))
        pruneUnsubscribedTranscripts(queryClient, names)
        retarget()
      }, subscriptionSettle)
    },
    close() {
      active = false
      events?.close()
      cancelConnecting()
      if (reconnectTimer !== null) timers.clearTimeout(reconnectTimer)
      if (settleTimer !== null) timers.clearTimeout(settleTimer)
      if (watchdog !== null) timers.clearInterval(watchdog)
      transcriptRefreshTimers.forEach((timer) => timers.clearTimeout(timer))
      transcriptRefreshTimers.clear()
    },
  }
}

export function deferFleetSubscription(
  subscribe: () => () => void,
  scheduleFrame: (callback: () => void) => number = (callback) => window.requestAnimationFrame(callback),
  cancelFrame: (handle: number) => void = (handle) => window.cancelAnimationFrame(handle),
  scheduleSettle: (callback: () => void) => number = (callback) => window.setTimeout(callback, 60),
  cancelSettle: (handle: number) => void = (handle) => window.clearTimeout(handle),
) {
  let pending = true
  let stop: (() => void) | undefined
  let settleHandle: number | null = null
  const frameHandle = scheduleFrame(() => {
    settleHandle = scheduleSettle(() => {
      pending = false
      stop = subscribe()
    })
  })
  return () => {
    if (pending) {
      cancelFrame(frameHandle)
      if (settleHandle !== null) cancelSettle(settleHandle)
    }
    stop?.()
  }
}

// useFleetStream opens the page's one event stream once and retargets it
// as the subscription changes.
export function useFleetStream(targets: FleetTargets, onStateChanged?: (namespace: string, rev: number) => void) {
  const queryClient = useQueryClient()
  const previousScreenSubscription = useRef('')
  const stream = useRef<FleetStream | null>(null)
  const latestKey = useRef('')
  const latestStateChanged = useRef(onStateChanged)
  const key = fleetTargetsKey(targets)
  const screenSubscription = [...new Set(targets.screens ?? [])].sort().join(',')
  useEffect(() => {
    const current = screenSubscription ? screenSubscription.split(',') : []
    if (previousScreenSubscription.current) {
      unsubscribedScreenPaneIDs(previousScreenSubscription.current.split(','), current).forEach((paneID) => {
        queryClient.removeQueries({ queryKey: queryKeys.screen(paneID), exact: true })
      })
    }
    previousScreenSubscription.current = screenSubscription
  }, [queryClient, screenSubscription])
  useEffect(() => {
    latestStateChanged.current = onStateChanged
  }, [onStateChanged])
  useEffect(() => {
    latestKey.current = key
    stream.current?.update(targetsFromKey(key))
  }, [key])
  useEffect(() => deferFleetSubscription(() => {
    const subscription = subscribeToFleet(queryClient, targetsFromKey(latestKey.current), {
      onStateChanged: (namespace, rev) => latestStateChanged.current?.(namespace, rev),
    })
    stream.current = subscription
    return () => {
      stream.current = null
      subscription.close()
    }
  }), [queryClient])
}

export function useStreamStatus() {
  return useQuery({
    queryKey: queryKeys.stream,
    queryFn: async () => initialStreamState,
    initialData: initialStreamState,
    staleTime: Infinity,
    notifyOnChangeProps: ['data'],
  }).data
}

export function useStreamAlerts(): StreamAlerts {
  return useQuery({
    queryKey: queryKeys.stream,
    queryFn: async () => initialStreamState,
    initialData: initialStreamState,
    staleTime: Infinity,
    select: streamAlerts,
    notifyOnChangeProps: ['data'],
  }).data
}
