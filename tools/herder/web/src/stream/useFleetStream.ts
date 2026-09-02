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

export function eventStreamURL(agentNames: string[], screenPaneIDs: string[] = [], fileWatches: FileWatchTarget[] = [], focusedScreenPaneID?: string) {
  const agents = [...new Set(agentNames)].sort().join(',')
  const screens = [...new Set(screenPaneIDs)].sort().join(',')
  const query = new URLSearchParams()
  if (agents) query.set('agents', agents)
  if (screens) query.set('screens', screens)
  if (fileWatches.length > 0) query.set('watches', JSON.stringify(fileWatches))
  if (focusedScreenPaneID) query.set('focused_screen', focusedScreenPaneID)
  return query.size ? `/api/events?${query}` : '/api/events'
}

export function unsubscribedScreenPaneIDs(previous: string[], current: string[]) {
  const subscribed = new Set(current)
  return [...new Set(previous)].filter((paneID) => !subscribed.has(paneID))
}

export function subscribeToFleet(
  queryClient: QueryClient,
  agentNames: string[],
  screenPaneIDs: string[] = [],
  fileWatches: FileWatchTarget[] = [],
  focusedScreenPaneID?: string,
  createEventSource: (url: string) => EventSourceLike = (url) => new EventSource(url),
  timers: TimerHost = window,
  onStateChanged?: (namespace: string, rev: number) => void,
) {
  let active = true
  let events: EventSourceLike | null = null
  let reconnectTimer: number | null = null
  let watchdog: number | null = null
  const transcriptRefreshTimers = new Map<string, number>()
  let backoff = 500
  let hasOpened = false
  let lastActivity = Date.now()
  const names = [...new Set(agentNames)].sort()
  const panes = [...new Set(screenPaneIDs)].sort()
  const invalidateFileWatch = (fact: FileWatchTarget) => {
    if (fact.kind === 'file') {
      void queryClient.invalidateQueries({ queryKey: queryKeys.file(fact.root, fact.path), exact: true })
      return
    }
    void queryClient.invalidateQueries({ queryKey: queryKeys.fileTree(fact.root, fact.path), exact: true })
    void queryClient.invalidateQueries({ queryKey: queryKeys.backlog(fact.root, fact.path), exact: true })
  }
  const scheduleTranscriptRefresh = (name: string) => {
    if (transcriptRefreshTimers.has(name)) return
    const timer = timers.setTimeout(() => {
      transcriptRefreshTimers.delete(name)
      void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true })
      void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
    }, transcriptBurstDebounce)
    transcriptRefreshTimers.set(name, timer)
  }
  const update = (change: (current: StreamState) => StreamState) => {
    queryClient.setQueryData<StreamState>(queryKeys.stream, (current) => change(current ?? initialStreamState))
  }
  update((current) => ({ ...current, problems: withoutUnsubscribedTranscripts(current.problems, names) }))
  const touch = (visible = true) => {
    lastActivity = Date.now()
    if (visible) update((current) => ({ ...current, lastEvent: lastActivity }))
  }
  const scheduleReconnect = (detail: string) => {
    if (!active || reconnectTimer !== null) return
    events?.close()
    events = null
    update((current) => ({ ...current, problems: { ...current.problems, stream: detail } }))
    reconnectTimer = timers.setTimeout(() => {
      reconnectTimer = null
      connect()
    }, backoff)
    backoff = Math.min(backoff * 2, 10_000)
  }
  const connect = () => {
    if (!active) return
    lastActivity = Date.now()
    update((current) => ({
      ...current,
      substrateProof: { herdr: false, hcom: false },
      problems: { ...current.problems, stream: 'Connecting to live fleet…' },
    }))
    try {
      events = createEventSource(eventStreamURL(names, panes, fileWatches, focusedScreenPaneID))
    } catch {
      scheduleReconnect('Live stream disconnected; reconnecting…')
      return
    }
    events.onopen = () => {
      touch()
      backoff = 500
      update((current) => ({ ...current, problems: without(current.problems, 'stream') }))
      const catchUp = hasOpened
      hasOpened = true
      if (catchUp) {
        names.forEach((name) => {
          void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true })
          void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
        })
        fileWatches.forEach(invalidateFileWatch)
      }
    }
    events.onerror = () => scheduleReconnect('Live stream disconnected; reconnecting…')
    events.addEventListener('hello', (event) => {
      touch()
      const { buildIdentity } = JSON.parse(event.data) as { buildIdentity: string }
      update((current) => recordBuildIdentity(current, buildIdentity))
    })
    events.addEventListener('ping', () => touch(false))
    events.addEventListener('state-changed', (event) => {
      touch()
      const change = JSON.parse(event.data) as { namespace: string, rev: number }
      onStateChanged?.(change.namespace, change.rev)
    })
    events.addEventListener('fleet', (event) => {
      touch()
      queryClient.setQueryData<Board>(queryKeys.fleet, JSON.parse(event.data) as Board)
      names.forEach((name) => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
        void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true })
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
      to?.filter((name) => names.includes(name)).forEach((name) => {
        if (deferMessageRefresh(queryClient, name)) return
        void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
        void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true })
      })
    })
    events.addEventListener('rewindow', (event) => {
      touch()
      const { agent } = JSON.parse(event.data) as { agent: string }
      if (!names.includes(agent)) return
      void queryClient.resetQueries({ queryKey: queryKeys.entries(agent), exact: true })
      void queryClient.invalidateQueries({ queryKey: queryKeys.agent(agent), exact: true })
    })
    names.forEach((name) => events?.addEventListener(`entry:${name}`, () => {
      touch()
      scheduleTranscriptRefresh(name)
    }))
    panes.forEach((paneID) => events?.addEventListener(`screen:${paneID}`, (event) => {
      touch()
      queryClient.setQueryData<ScreenFrame>(queryKeys.screen(paneID), JSON.parse(event.data) as ScreenFrame)
    }))
    events.addEventListener('file-change', (event) => {
      touch()
      const fact = JSON.parse(event.data) as FileWatchTarget
      if (fact.kind === 'file' || fact.kind === 'folder') invalidateFileWatch(fact)
    })
  }

  connect()
  watchdog = timers.setInterval(() => {
    if (Date.now() - lastActivity > 45_000) scheduleReconnect('Live stream timed out; reconnecting…')
  }, 5_000)

  return () => {
    active = false
    events?.close()
    if (reconnectTimer !== null) timers.clearTimeout(reconnectTimer)
    if (watchdog !== null) timers.clearInterval(watchdog)
    transcriptRefreshTimers.forEach((timer) => timers.clearTimeout(timer))
    transcriptRefreshTimers.clear()
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

export function useFleetStream(agentNames: string[], screenPaneIDs: string[] = [], fileWatches: FileWatchTarget[] = [], focusedScreenPaneID?: string, onStateChanged?: (namespace: string, rev: number) => void) {
  const queryClient = useQueryClient()
  const previousScreenSubscription = useRef('')
  const subscription = [...new Set(agentNames)].sort().join(',')
  const screenSubscription = [...new Set(screenPaneIDs)].sort().join(',')
  const fileWatchSubscription = JSON.stringify(fileWatches)
  useEffect(() => {
    const current = screenSubscription ? screenSubscription.split(',') : []
    if (previousScreenSubscription.current) {
      unsubscribedScreenPaneIDs(previousScreenSubscription.current.split(','), current).forEach((paneID) => {
        queryClient.removeQueries({ queryKey: queryKeys.screen(paneID), exact: true })
      })
    }
    previousScreenSubscription.current = screenSubscription
  }, [queryClient, screenSubscription])
  useEffect(() => deferFleetSubscription(() => subscribeToFleet(
    queryClient,
    subscription ? subscription.split(',') : [],
    screenSubscription ? screenSubscription.split(',') : [],
    JSON.parse(fileWatchSubscription) as FileWatchTarget[],
    focusedScreenPaneID, undefined, window, onStateChanged,
  )), [fileWatchSubscription, focusedScreenPaneID, onStateChanged, queryClient, subscription, screenSubscription])
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
