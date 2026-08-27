import { useEffect } from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { queryKeys } from '../api/client.ts'
import type { Board, ScreenFrame, SubstrateEvent } from '../types'

export type StreamState = {
  problems: Record<string, string>
  messages: number
  lastEvent: number | null
  loadedBuild: string | null
  serverUpdated: boolean
}

const initialStreamState: StreamState = {
  problems: { stream: 'Connecting to live fleet…' },
  messages: 0,
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

function without(problem: Record<string, string>, key: string) {
  const next = { ...problem }
  delete next[key]
  return next
}

export function withoutUnsubscribedTranscripts(problems: Record<string, string>, agentNames: string[]) {
  const subscribed = new Set(agentNames.map((name) => `transcript:${name}`))
  return Object.fromEntries(Object.entries(problems).filter(([source]) => !source.startsWith('transcript:') || subscribed.has(source)))
}

export function eventStreamURL(agentNames: string[], screenPaneIDs: string[] = []) {
  const agents = [...new Set(agentNames)].sort().join(',')
  const screens = [...new Set(screenPaneIDs)].sort().join(',')
  const query = new URLSearchParams()
  if (agents) query.set('agents', agents)
  if (screens) query.set('screens', screens)
  return query.size ? `/api/events?${query}` : '/api/events'
}

export function subscribeToFleet(
  queryClient: QueryClient,
  agentNames: string[],
  screenPaneIDs: string[] = [],
  createEventSource: (url: string) => EventSourceLike = (url) => new EventSource(url),
  timers: TimerHost = window,
) {
  let active = true
  let events: EventSourceLike | null = null
  let reconnectTimer: number | null = null
  let watchdog: number | null = null
  let backoff = 500
  let lastActivity = Date.now()
  const names = [...new Set(agentNames)].sort()
  const panes = [...new Set(screenPaneIDs)].sort()
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
    update((current) => current.loadedBuild ? current : { ...current, problems: { ...current.problems, stream: 'Connecting to live fleet…' } })
    try {
      events = createEventSource(eventStreamURL(names, panes))
    } catch {
      scheduleReconnect('Live stream disconnected; reconnecting…')
      return
    }
    events.onopen = () => {
      touch()
      backoff = 500
      update((current) => ({ ...current, problems: without(current.problems, 'stream') }))
      names.forEach((name) => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true })
        void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
      })
    }
    events.onerror = () => scheduleReconnect('Live stream disconnected; reconnecting…')
    events.addEventListener('hello', (event) => {
      touch()
      const { buildIdentity } = JSON.parse(event.data) as { buildIdentity: string }
      update((current) => recordBuildIdentity(current, buildIdentity))
    })
    events.addEventListener('ping', () => touch(false))
    events.addEventListener('fleet', (event) => {
      touch()
      queryClient.setQueryData<Board>(queryKeys.fleet, JSON.parse(event.data) as Board)
      names.forEach((name) => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
        void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true })
      })
      update((current) => ({ ...current, problems: without(current.problems, 'fleet') }))
    })
    events.addEventListener('substrate', (event) => {
      touch()
      const state = JSON.parse(event.data) as SubstrateEvent
      update((current) => ({
        ...current,
        problems: state.status === 'recovered'
          ? without(current.problems, state.source)
          : { ...current.problems, [state.source]: state.detail ?? `${state.source} is unreachable` },
      }))
    })
    events.addEventListener('message', (event) => {
      touch()
      update((current) => ({ ...current, messages: current.messages + 1 }))
      const { to } = JSON.parse(event.data) as { to?: string[] }
      to?.filter((name) => names.includes(name)).forEach((name) => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
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
      void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true })
      void queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true })
    }))
    panes.forEach((paneID) => events?.addEventListener(`screen:${paneID}`, (event) => {
      touch()
      queryClient.setQueryData<ScreenFrame>(queryKeys.screen(paneID), JSON.parse(event.data) as ScreenFrame)
    }))
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
  }
}

export function useFleetStream(agentNames: string[], screenPaneIDs: string[] = []) {
  const queryClient = useQueryClient()
  const subscription = [...new Set(agentNames)].sort().join(',')
  const screenSubscription = [...new Set(screenPaneIDs)].sort().join(',')
  const stream = useQuery({
    queryKey: queryKeys.stream,
    queryFn: async () => initialStreamState,
    initialData: initialStreamState,
    staleTime: Infinity,
  }).data

  useEffect(() => subscribeToFleet(queryClient, subscription ? subscription.split(',') : [], screenSubscription ? screenSubscription.split(',') : []), [queryClient, subscription, screenSubscription])
  return stream
}
