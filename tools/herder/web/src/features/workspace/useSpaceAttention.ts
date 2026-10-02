import { useCallback, useEffect, useMemo, useState, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { queryKeys } from '../../api/client'
import type { Board, EntriesPage } from '../../types'
import { useDOMEvent } from '../../shared/lifecycle'
import { panelParams } from '../layout/dockLayout'
import {
  dwelledAgents,
  latestPosition,
  markLastTurnUnread,
  markUnreadAt,
  markerKeepSet,
  nextArmed,
  nextDwellDelay,
  nextViewing,
  readUpdates,
  seedUpdates,
  spaceAttention,
  storedSpaceAgents,
  useReadMarkers,
  useReadMarkersContext,
  viewedAgents,
  type ReadPosition,
  type SpaceAttention,
  type SpaceDefinition,
  type ViewingState,
} from '../spaces/index.ts'

function documentVisible() {
  return typeof document === 'undefined' || document.visibilityState === 'visible'
}

// transcriptEnd is the newest entry a viewed agent's transcript has
// rendered: undefined while it is still loading, null when it has none or
// cannot load (reading then advances the turn alone).
function transcriptEnd(queryClient: QueryClient, name: string): ReadPosition | null | undefined {
  const page = queryClient.getQueryData<EntriesPage>(queryKeys.entries(name))
  if (page) return latestPosition(page)
  return queryClient.getQueryState(queryKeys.entries(name))?.status === 'error' ? null : undefined
}

// useSpaceAttention derives each space's waiting/blocked agents from the
// live dock (active space), the stored layouts (other spaces), the fleet
// board and the shared read markers, and records reading for the dwell.
export function useSpaceAttention({ apiRef, revision, board, spaces, activeSpaceID, activeAgents }: {
  apiRef: MutableRefObject<DockviewApi | undefined>
  revision: number
  board: Board | undefined
  spaces: SpaceDefinition[]
  activeSpaceID: string | null
  activeAgents: string[]
}) {
  const queryClient = useQueryClient()
  const { store, pulled } = useReadMarkersContext()
  const markers = useReadMarkers()
  const [visible, setVisible] = useState(documentVisible)
  const [storageTick, setStorageTick] = useState(0)
  const [viewing, setViewing] = useState<ViewingState>({})
  const [dwellTick, setDwellTick] = useState(0)
  const [entriesTick, setEntriesTick] = useState(0)
  const [armed, setArmed] = useState<ReadonlySet<string>>(() => new Set())

  useDOMEvent(document, 'visibilitychange', () => setVisible(documentVisible()))
  useDOMEvent<StorageEvent>(window, 'storage', (event) => {
    if (event.key?.startsWith('herder.web.layout.v4')) setStorageTick((tick) => tick + 1)
  })

  const activeKey = activeAgents.join('\n')
  const openBySpace = useMemo(() => {
    const result: Record<string, string[]> = {}
    for (const space of spaces) {
      result[space.id] = space.id === activeSpaceID ? activeKey.split('\n').filter(Boolean) : storedSpaceAgents(localStorage, space.id)
    }
    return result
    // revision covers stored layouts rewritten by a switch or a send-to-space.
  }, [activeKey, activeSpaceID, revision, spaces, storageTick])

  const api = apiRef.current
  const groups = api?.groups ?? []
  const viewedKey = viewedAgents(groups.map((group) => {
    const params = panelParams(group.activePanel?.params)
    return { visible: group.api.isVisible, activeAgent: params?.kind === 'agent' ? params.name : undefined }
  }), visible).join('\n')
  // A reload restoring the viewed panel is not leaving it: arming waits
  // for a dock with groups in a visible document.
  const dockReady = Boolean(api) && groups.length > 0 && visible
  useEffect(() => {
    setViewing((previous) => nextViewing(previous, viewedKey.split('\n').filter(Boolean), Date.now()))
  }, [viewedKey])
  useEffect(() => {
    setArmed((previous) => nextArmed(previous, markers, viewedKey.split('\n').filter(Boolean), dockReady))
  }, [dockReady, markers, viewedKey])
  useEffect(() => {
    const delay = nextDwellDelay(viewing, Date.now())
    if (delay === null) return
    const timer = window.setTimeout(() => setDwellTick((tick) => tick + 1), delay)
    return () => window.clearTimeout(timer)
  }, [dwellTick, viewing])
  // New entries in a viewed transcript move the read position.
  useEffect(() => {
    const viewed = new Set(viewedKey.split('\n').filter(Boolean))
    if (viewed.size === 0) return
    return queryClient.getQueryCache().subscribe((event) => {
      const [kind, name] = event.query.queryKey as readonly unknown[]
      if (event.type === 'updated' && kind === 'entries' && typeof name === 'string' && viewed.has(name)) setEntriesTick((tick) => tick + 1)
    })
  }, [queryClient, viewedKey])

  useEffect(() => {
    const open = Object.values(openBySpace).flat()
    const keep = markerKeepSet(board, open)
    if (keep) store.prune(keep)
    // Seeding waits for the first pull so a fresh browser never quietly
    // overwrites what another device has not read yet.
    if (pulled) store.apply(seedUpdates(store.markers(), board, open), { weak: true })
    const dwelled = dwelledAgents(viewing, Date.now())
    const positions = Object.fromEntries(dwelled.map((name) => [name, transcriptEnd(queryClient, name)]))
    store.apply(readUpdates({ markers: store.markers(), board, viewed: dwelled, positions, armed, now: Date.now() }))
  }, [armed, board, dwellTick, entriesTick, openBySpace, pulled, queryClient, store, viewing])

  // markUnread marks an agent unread from entries[index] of its transcript
  // window ("Mark unread from here"), or without an index from the start of
  // its latest turn (the footer, the menus and ⌥U). The mark holds until
  // the agent is left and come back to.
  const markUnread = useCallback((name: string, index?: number) => {
    const page = queryClient.getQueryData<EntriesPage>(queryKeys.entries(name))
    const marker = store.markers()[name]
    store.apply({ [name]: index === undefined ? markLastTurnUnread(marker, page) : markUnreadAt(marker, page, index) })
    setArmed((previous) => {
      if (!previous.has(name)) return previous
      const next = new Set(previous)
      next.delete(name)
      return next
    })
  }, [queryClient, store])

  const attention = useMemo(() => Object.fromEntries(Object.entries(openBySpace)
    .map(([id, agents]) => [id, spaceAttention(board, agents, markers)])) as Record<string, SpaceAttention>, [board, markers, openBySpace])
  return { attention, markUnread }
}
