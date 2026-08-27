import { useEffect } from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { queryKeys } from '../api/client.ts'
import type { Board, SubstrateEvent } from '../types'

export type StreamState = {
  problems: Record<string, string>
  messages: number
  lastEvent: number | null
}

const initialStreamState: StreamState = {
  problems: { stream: 'Connecting to live fleet…' },
  messages: 0,
  lastEvent: null,
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

export function eventStreamURL(agentNames: string[]) {
  const subscription = [...new Set(agentNames)].sort().join(',')
  return subscription ? `/api/events?${new URLSearchParams({ agents: subscription })}` : '/api/events'
}

export function subscribeToFleet(
  queryClient: QueryClient,
  agentNames: string[],
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
  const update = (change: (current: StreamState) => StreamState) => {
    queryClient.setQueryData<StreamState>(queryKeys.stream, (current) => change(current ?? initialStreamState))
  }
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
    update((current) => ({ ...current, problems: { ...current.problems, stream: 'Connecting to live fleet…' } }))
    try {
      events = createEventSource(eventStreamURL(names))
    } catch {
      scheduleReconnect('Live stream disconnected; reconnecting…')
      return
    }
    events.onopen = () => {
      touch()
      backoff = 500
      update((current) => ({ ...current, problems: without(current.problems, 'stream') }))
      names.forEach((name) => void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true }))
    }
    events.onerror = () => scheduleReconnect('Live stream disconnected; reconnecting…')
    events.addEventListener('ping', () => touch(false))
    events.addEventListener('fleet', (event) => {
      touch()
      queryClient.setQueryData<Board>(queryKeys.fleet, JSON.parse(event.data) as Board)
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
    events.addEventListener('message', () => {
      touch()
      update((current) => ({ ...current, messages: current.messages + 1 }))
    })
    events.addEventListener('rewindow', (event) => {
      touch()
      const { agent } = JSON.parse(event.data) as { agent: string }
      if (!names.includes(agent)) return
      void queryClient.resetQueries({ queryKey: queryKeys.entries(agent), exact: true })
    })
    names.forEach((name) => events?.addEventListener(`entry:${name}`, () => {
      touch()
      void queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true })
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

export function useFleetStream(agentNames: string[]) {
  const queryClient = useQueryClient()
  const subscription = [...new Set(agentNames)].sort().join(',')
  const stream = useQuery({
    queryKey: queryKeys.stream,
    queryFn: async () => initialStreamState,
    initialData: initialStreamState,
    staleTime: Infinity,
  }).data

  useEffect(() => subscribeToFleet(queryClient, subscription ? subscription.split(',') : []), [queryClient, subscription])
  return stream
}
