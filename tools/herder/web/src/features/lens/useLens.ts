import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Board } from '../../types.ts'
import { findAgentRow } from '../../shared/agentStatus.ts'
import { bindLensShortcuts, type LensKeyActions } from '../layout/shellShortcuts.ts'
import { needsYou, type SpaceAttention, type SpaceDefinition, type SpaceMember } from '../spaces/index.ts'
import {
  cardLook,
  cardShape,
  cycleVisible,
  entryAgent,
  lensOrder,
  lensRows,
  lensRowsOf,
  lensStatus,
  liveMembers,
  looseAgents,
  nextNeeding,
  rowHeads,
  selectedSpace,
  spaceStep,
  statusText,
  stepCard,
  stepRow,
  visibleAgent,
  whoLine,
  type CardLook,
  type LensPrefs,
  type LensRow,
  type LensStatus,
  type LensView,
} from './lensModel.ts'
import { readLensPrefs, writeLensPrefs } from './lensPrefs.ts'

// LensCard is everything one card draws. text is the status fallback; the
// last-answer text (lens-cards) slots in here.
export type LensCard = {
  id: string
  name: string
  needs: number
  look: CardLook
  agent: string
  status: LensStatus | null
  others: number
  text: string
  width: number
  lines: number
}

export type LensRowView = { row: LensRow, index: number, head: string, cards: LensCard[] }

const quiet: SpaceAttention = { unread: [], blocked: [], drafts: [] }

// useLens owns the lens: its rows and visible agents (this browser's
// prefs), the selection, the cards and every lens and zoom key. The
// workspace controller owns the view and the zoom itself.
export function useLens({ enabled, spaces, membersBySpace, board, attention, total, view, activeSpaceID, selected, setSelected, zoomInto, zoomOut, openAlone, markSpaceRead, markSpaceUnread }: {
  enabled: boolean
  spaces: SpaceDefinition[]
  membersBySpace: Record<string, SpaceMember[]>
  board: Board | undefined
  attention: Record<string, SpaceAttention>
  total: SpaceAttention
  view: LensView
  activeSpaceID: string | null
  selected: string | null
  setSelected: (id: string | null) => void
  zoomInto: (spaceID: string, agent?: string) => boolean
  zoomOut: () => boolean
  openAlone: (agent: string) => void
  markSpaceRead: (spaceID: string) => void
  markSpaceUnread: (spaceID: string) => void
}) {
  const [initial] = useState(() => readLensPrefs(localStorage))
  const [prefs, setPrefs] = useState<LensPrefs>(initial.prefs)
  const prefsState = useRef(initial.state)
  useEffect(() => { prefsState.current = writeLensPrefs(localStorage, prefs, prefsState.current) }, [prefs])
  // revealTick asks the lens to bring the selection into view again.
  const [revealTick, setRevealTick] = useState(0)

  const order = useMemo(() => lensOrder(spaces, prefs), [prefs, spaces])
  const chosen = selectedSpace(order, selected)
  const needing = useMemo(() => new Set([...total.unread, ...total.blocked]), [total])
  const live = useCallback((id: string) => liveMembers(membersBySpace[id] ?? [], board), [board, membersBySpace])
  const entry = useCallback((id: string) => entryAgent(live(id), prefs.visible[id], (name) => needing.has(name)), [live, needing, prefs.visible])

  const rows = useMemo((): LensRowView[] => {
    const grouped = lensRowsOf(spaces, prefs)
    return lensRows.map((row, index) => ({
      row, index, head: `${index + 1} ${rowHeads[row]} · ${grouped[row].length}`,
      cards: grouped[row].map((space): LensCard => {
        const members = membersBySpace[space.id] ?? []
        const agent = visibleAgent(liveMembers(members, board), prefs.visible[space.id])
        const agentRow = agent ? findAgentRow(board, agent) : undefined
        const status = agentRow ? lensStatus(agentRow) : null
        const needs = needsYou(attention[space.id] ?? quiet)
        const who = whoLine(members, agent)
        return {
          id: space.id, name: space.name, needs, look: cardLook(needs, space.id === chosen, status),
          agent: who.name, status, others: who.others,
          text: statusText(agentRow, agent !== undefined && needing.has(agent)),
          ...cardShape(row),
        }
      }),
    }))
  }, [attention, board, chosen, membersBySpace, needing, prefs, spaces])

  const select = useCallback((id: string) => {
    setSelected(id)
    setRevealTick((tick) => tick + 1)
  }, [setSelected])
  const zoomCard = useCallback((id: string) => {
    setSelected(id)
    return zoomInto(id, entry(id))
  }, [entry, setSelected, zoomInto])

  // A step to a space in lens order from the zoomed one, wrapping: [ ] and ⇧⌥←/→.
  const stepSpace = useCallback((by: -1 | 1) => {
    if (view.kind !== 'zoom') return false
    const next = spaceStep(order, activeSpaceID, by)
    return next ? zoomCard(next) : false
  }, [activeSpaceID, order, view.kind, zoomCard])

  const latest = useRef<LensKeyActions | null>(null)
  const onLens = enabled && view.kind === 'lens'
  const lensKey = (action: (id: string) => void) => () => {
    if (!onLens || !chosen) return false
    action(chosen)
    setRevealTick((tick) => tick + 1)
    return true
  }
  latest.current = {
    step: (by) => lensKey((id) => setSelected(stepCard(order, id, by)))(),
    stepRow: (by) => lensKey((id) => setSelected(stepRow(spaces, prefs, id, by)))(),
    zoomIn: () => onLens && chosen ? zoomCard(chosen) : false,
    zoomOut: () => enabled && view.kind !== 'lens' ? zoomOut() : false,
    nextNeeding: (zoom) => {
      if (!enabled) return false
      const from = view.kind === 'alone' ? { kind: 'alone' as const, agent: view.agent }
        : view.kind === 'zoom' ? activeSpaceID ? { kind: 'space' as const, id: activeSpaceID } : null
          : chosen ? { kind: 'space' as const, id: chosen } : null
      const stop = nextNeeding({
        order, loose: looseAgents(needing, membersBySpace), from, alone: zoom || view.kind !== 'lens',
        spaceNeeds: (id) => needsYou(attention[id] ?? quiet) > 0, agentNeeds: (name) => needing.has(name),
      })
      if (!stop) return false
      if (stop.kind === 'alone') { openAlone(stop.agent); return true }
      if (zoom || view.kind !== 'lens') return zoomCard(stop.id)
      select(stop.id)
      return true
    },
    place: (row) => lensKey((id) => {
      setSelected(id)
      setPrefs((current) => ({ ...current, rows: { ...current.rows, [id]: lensRows[row]! } }))
    })(),
    cycleVisible: () => lensKey((id) => {
      const next = cycleVisible(live(id), prefs.visible[id])
      if (next) setPrefs((current) => ({ ...current, visible: { ...current.visible, [id]: next } }))
    })(),
    markRead: () => lensKey(markSpaceRead)(),
    markUnread: () => lensKey(markSpaceUnread)(),
    stepSpace,
  }
  useEffect(() => bindLensShortcuts(window, {
    step: (by) => latest.current?.step(by),
    stepRow: (by) => latest.current?.stepRow(by),
    zoomIn: () => latest.current?.zoomIn(),
    zoomOut: () => latest.current?.zoomOut(),
    nextNeeding: (zoom) => latest.current?.nextNeeding(zoom),
    place: (row) => latest.current?.place(row),
    cycleVisible: () => latest.current?.cycleVisible(),
    markRead: () => latest.current?.markRead(),
    markUnread: () => latest.current?.markUnread(),
    stepSpace: (by) => latest.current?.stepSpace(by),
  }), [])

  // A zoom's agent opened from a mention while alone: the first space in lens
  // order holding it, else alone again.
  const summon = useCallback((agent: string) => {
    const home = order.find((id) => (membersBySpace[id] ?? []).some((member) => member.kind === 'agent' && member.name === agent))
    if (home) zoomInto(home, agent)
    else openAlone(agent)
  }, [membersBySpace, openAlone, order, zoomInto])

  return { prefs, order, rows, selected: chosen, select, zoomCard, stepSpace, summon, revealTick, needYou: needsYou(total) }
}
