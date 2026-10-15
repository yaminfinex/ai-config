import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import type { DockviewApi } from 'dockview-react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { queryKeys } from '../../api/client'
import type { Board, EntriesPage } from '../../types'
import { useDOMEvent } from '../../shared/lifecycle'
import { panelParams } from '../layout/dockLayout'
import { findAgentRow } from '../../shared/agentStatus'
import { liveMembers, readViewedBlocks, writeViewedBlocks, type LensView } from '../lens/index.ts'
import {
  agentUnread,
  boardAgents,
  boardAttention,
  dwelledAgents,
  latestPosition,
  markLastTurnUnread,
  markReadUpdates,
  markUnreadAt,
  markerKeepSet,
  nextArmed,
  nextDwellDelay,
  nextViewedBlocks,
  nextViewing,
  readThrough,
  readUpdates,
  seedUpdates,
  spaceAttention,
  turnEnd,
  useReadMarkers,
  useReadMarkersContext,
  viewedAgents,
  type ReadPosition,
  type SpaceAttention,
  type SpaceMember,
  type ViewingState,
} from '../spaces/index.ts'
import { useDraftMarks } from '../drafts/useDraftMarks.ts'

function documentVisible() {
  return typeof document === 'undefined' || document.visibilityState === 'visible'
}

// transcriptEnd is the newest entry a viewed agent's transcript has
// rendered: undefined while it is still loading, null when it has none or
// cannot load (reading then advances the turn alone).
export type SeenAgent = { turn: number | null, position: ReadPosition | null }

function transcriptEnd(queryClient: QueryClient, name: string): ReadPosition | null | undefined {
  const page = queryClient.getQueryData<EntriesPage>(queryKeys.entries(name))
  if (page) return latestPosition(page)
  return queryClient.getQueryState(queryKeys.entries(name))?.status === 'error' ? null : undefined
}

// useSpaceAttention derives each space's waiting/blocked/draft agents from
// its members on the board (spaces.members), the board-wide total, the
// fleet board and the shared read markers; it records reading for the
// dwell and keeps the viewed-blocks set. Only the focused panel is seen:
// the zoom's focused group, the agent opened alone, nothing on the lens.
export function useSpaceAttention({ apiRef, board, openBySpace, membersBySpace, view }: {
  apiRef: MutableRefObject<DockviewApi | undefined>
  board: Board | undefined
  openBySpace: Record<string, string[]>
  membersBySpace: Record<string, SpaceMember[]>
  view: LensView
}) {
  const queryClient = useQueryClient()
  const { store, pulled } = useReadMarkersContext()
  const markers = useReadMarkers()
  const drafts = useDraftMarks()
  const [visible, setVisible] = useState(documentVisible)
  const [viewing, setViewing] = useState<ViewingState>({})
  const [dwellTick, setDwellTick] = useState(0)
  const [entriesTick, setEntriesTick] = useState(0)
  const [armed, setArmed] = useState<ReadonlySet<string>>(() => new Set())
  const [initialBlocks] = useState(() => readViewedBlocks(localStorage))
  const [viewedBlocks, setViewedBlocks] = useState(initialBlocks.blocks)
  const blocksState = useRef(initialBlocks.state)

  useDOMEvent(document, 'visibilitychange', () => setVisible(documentVisible()))

  const api = apiRef.current
  const groups = api?.groups ?? []
  const activeGroupID = api?.activeGroup?.id
  const viewedKey = view.kind === 'lens' ? ''
    : view.kind === 'alone' ? visible ? view.agent : ''
      : viewedAgents(groups.map((group) => {
        const params = panelParams(group.activePanel?.params)
        return { visible: group.api.isVisible, focused: group.id === activeGroupID, activeAgent: params?.kind === 'agent' ? params.name : undefined }
      }), visible).join('\n')
  // A reload restoring the viewed panel is not leaving it: arming waits
  // for a dock with groups in a visible document. The lens views nothing,
  // so marks arm there at once.
  const dockReady = visible && (view.kind !== 'zoom' || (Boolean(api) && groups.length > 0))
  useEffect(() => {
    setViewing((previous) => nextViewing(previous, viewedKey.split('\n').filter(Boolean), Date.now()))
  }, [viewedKey])
  useEffect(() => {
    setArmed((previous) => nextArmed(previous, markers, viewedKey.split('\n').filter(Boolean), dockReady))
  }, [dockReady, markers, viewedKey])
  // Viewing a blocked agent views its block at once, and a block landing
  // while it is viewed too; unblocking drops the mark.
  const memberKey = Object.values(membersBySpace).flat().flatMap((member) => member.kind === 'agent' ? [member.name] : []).join('\n')
  useEffect(() => {
    const members = new Set(memberKey.split('\n'))
    setViewedBlocks((previous) => nextViewedBlocks(previous, board, viewedKey.split('\n').filter(Boolean), (name) => members.has(name)))
  }, [board, memberKey, viewedKey])
  useEffect(() => {
    blocksState.current = writeViewedBlocks(localStorage, viewedBlocks, blocksState.current)
  }, [viewedBlocks])
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
    // overwrites what another device has not read yet. Every board agent is
    // seeded, so one in no space can turn unread and count in the total.
    if (pulled) store.apply(seedUpdates(store.markers(), board, [...(board ? boardAgents(board) : []), ...open]), { weak: true })
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

  // markRead marks agents read now, as a dwell read would (a space's "Mark
  // all read" passes every agent counted in its badge); one already read is
  // left alone. It reads the board from the cache so a fleet refresh does not
  // change it, and with it every panel's callbacks.
  const markRead = useCallback((names: readonly string[]) => {
    const positions = Object.fromEntries(names.map((name) => [name, transcriptEnd(queryClient, name)]))
    store.apply(markReadUpdates({ markers: store.markers(), board: queryClient.getQueryData<Board>(queryKeys.fleet), names, positions, now: Date.now() }))
  }, [queryClient, store])

  // toggleRead is ⌥U: an unread agent is marked read, a read one unread.
  const toggleRead = useCallback((name: string) => {
    if (agentUnread(findAgentRow(queryClient.getQueryData<Board>(queryKeys.fleet), name), store.markers()[name])) markRead([name])
    else markUnread(name)
  }, [markRead, markUnread, queryClient, store])

  // seenNow is what the owner has seen of an agent: its latest turn and its
  // transcript's end. readThroughSeen reads up to that and no further, so
  // a turn the send itself wakes stays to be read (⌘⇧⏎).
  const seenNow = useCallback((name: string): SeenAgent => ({
    turn: turnEnd(findAgentRow(queryClient.getQueryData<Board>(queryKeys.fleet), name)),
    position: transcriptEnd(queryClient, name) ?? null,
  }), [queryClient])
  const readThroughSeen = useCallback((name: string, seen: SeenAgent) => {
    const next = readThrough(store.markers()[name], seen.turn, seen.position, Date.now())
    if (next) store.apply({ [name]: next })
  }, [store])

  const attention = useMemo(() => Object.fromEntries(Object.entries(membersBySpace)
    .map(([id, members]) => [id, spaceAttention(board, liveMembers(members, board), markers, drafts, viewedBlocks)])) as Record<string, SpaceAttention>, [board, drafts, markers, membersBySpace, viewedBlocks])
  const total = useMemo(() => boardAttention(board, markers, drafts, viewedBlocks), [board, drafts, markers, viewedBlocks])
  // viewBlocks views the named agents' current blocks, as looking at them would.
  const viewBlocks = useCallback((names: readonly string[]) => {
    const current = queryClient.getQueryData<Board>(queryKeys.fleet)
    setViewedBlocks((previous) => nextViewedBlocks(previous, current, names, (name) => previous.has(name)))
  }, [queryClient])
  // m: the space's unread members read, and their blocks viewed.
  const markSpaceRead = useCallback((spaceID: string) => {
    const space = attention[spaceID]
    if (!space) return
    markRead(space.unread)
    viewBlocks(space.blocked)
  }, [attention, markRead, viewBlocks])
  // u: every member on the board marked unread from its latest turn; each
  // mark holds until its agent is left and come back to (nextArmed).
  const markSpaceUnread = useCallback((spaceID: string) => {
    for (const name of liveMembers(membersBySpace[spaceID] ?? [], queryClient.getQueryData<Board>(queryKeys.fleet))) markUnread(name)
  }, [markUnread, membersBySpace, queryClient])
  return { attention, total, viewedBlocks, markUnread, markRead, toggleRead, markSpaceRead, markSpaceUnread, seenNow, readThroughSeen }
}
