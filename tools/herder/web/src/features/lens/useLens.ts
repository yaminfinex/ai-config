import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Board } from '../../types.ts'
import { findAgentRow } from '../../shared/agentStatus.ts'
import { bindLensShortcuts, type LensKeyActions } from '../layout/shellShortcuts.ts'
import { needsYou, turnEnd, type SpaceAttention, type SpaceDefinition, type SpaceMember } from '../spaces/index.ts'
import {
  cardLook,
  cardShape,
  cycleVisible,
  entryAgent,
  focusSizes,
  lensOrder,
  lensRows,
  lensRowsOf,
  lensStatus,
  rowOf,
  liveMembers,
  looseAgents,
  nextNeeding,
  readToggle,
  rowHeads,
  selectedSpace,
  spaceStep,
  statusText,
  stepCard,
  stepLine,
  stepRow,
  visibleAgent,
  whoLine,
  zoomLanding,
  type CardLook,
  type LensPrefs,
  type LensRow,
  type LensStatus,
  type LensView,
} from './lensModel.ts'
import { readLensPrefs, writeLensPrefs } from './lensPrefs.ts'
import { agentFolder, nextCardSize, nextCardText, type CardRead } from './cardTextModel.ts'
import { useCardText } from './useCardText.ts'
import { cardRects } from './LensView.tsx'
import { focusLens, leavePeek, type PeekTarget } from './LensPeek.tsx'
import { clampPeekWidth, peekWidth } from './peekModel.ts'

// LensCard is everything one card draws. text is the body: by t, the
// visible agent's last answer (the status until it loads), its status or
// its folder.
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
export function useLens({ enabled, spaces, membersBySpace, board, attention, total, view, activeSpaceID, selected, setSelected, lastPanel, zoomInto, zoomOut, openAlone, markSpaceRead, markSpaceUnread, readSeen }: {
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
  // lastPanel is the panel a space last had focused: the live dock's for the
  // active space, the saved layout's for the others.
  lastPanel: (spaceID: string) => { kind: string, name?: string } | undefined
  zoomInto: (spaceID: string, agent?: string, focus?: boolean, by?: -1 | 1) => boolean
  zoomOut: () => boolean
  openAlone: (agent: string) => void
  markSpaceRead: (spaceID: string) => void
  markSpaceUnread: (spaceID: string) => void
  // readSeen is called as a reply's send starts; its answer, once it lands,
  // marks the agent read up to what the owner had seen.
  readSeen: (agent: string) => () => void
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
  const text = prefs.text ?? 'answer'
  const size = prefs.size ?? 0
  // The text cards' agents (focus and watch), each with its turn, read only while the cards show answers.
  const carded = useMemo((): CardRead[] => text !== 'answer' ? [] : spaces.flatMap((space) => {
    if (rowOf(prefs, space.id) === 'background') return []
    const agent = visibleAgent(liveMembers(membersBySpace[space.id] ?? [], board), prefs.visible[space.id])
    return agent ? [{ agent, turn: turnEnd(findAgentRow(board, agent)) }] : []
  }), [board, membersBySpace, prefs, spaces, text])
  const onBoard = useCallback((agent: string) => Boolean(findAgentRow(board, agent)), [board])
  const answer = useCardText(carded, onBoard)
  const entry = useCallback((id: string) => zoomLanding(lastPanel(id), (name) => Boolean(findAgentRow(board, name)),
    () => entryAgent(live(id), prefs.visible[id], (name) => needing.has(name))), [board, lastPanel, live, needing, prefs.visible])

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
        const fallback = statusText(agentRow, agent !== undefined && needing.has(agent))
        const body = !agent || !agentRow ? fallback : text === 'cwd' ? agentFolder(board, agent) : text === 'answer' ? answer(agent) ?? fallback : fallback
        return {
          id: space.id, name: space.name, needs, look: cardLook(needs, space.id === chosen, status),
          agent: who.name, status, others: who.others, text: body,
          ...cardShape(row, size),
        }
      }),
    }))
  }, [answer, attention, board, chosen, membersBySpace, needing, prefs, size, spaces, text])

  // The peek follows the selection: its space and that space's visible agent.
  const peek = useMemo((): PeekTarget | null => {
    const space = chosen ? spaces.find((item) => item.id === chosen) : undefined
    if (!space) return null
    const agent = visibleAgent(live(space.id), prefs.visible[space.id])
    const agentRow = agent ? findAgentRow(board, agent) : undefined
    if (!agent || !agentRow) return { space: space.name, agent: null, status: null, title: '' }
    return { space: space.name, agent, status: lensStatus(agentRow), title: agentRow.title ?? '' }
  }, [board, chosen, live, prefs.visible, spaces])
  // The peek's reply (/): a composer for the peeked agent, open while its
  // card stays selected and shows that agent. focus asks it for the keys.
  const onLens = enabled && view.kind === 'lens'
  const [replying, setReplying] = useState<{ space: string, agent: string, focus: number } | null>(null)
  if (replying && (!onLens || replying.space !== chosen || replying.agent !== peek?.agent)) setReplying(null)
  const reply = useRef<() => boolean>(() => false)
  reply.current = () => {
    if (!onLens || !chosen || !peek?.agent) return false
    const target = { space: chosen, agent: peek.agent }
    setReplying((current) => ({ ...target, focus: (current?.focus ?? 0) + 1 }))
    return true
  }
  const startReply = useCallback(() => reply.current(), [])
  // leaveReply closes the reply (esc, or its send landed) and gives the keys
  // back to the cards; the selection stays and the draft is kept.
  const leaveReply = useCallback(() => {
    setReplying(null)
    focusLens()
  }, [])
  const replySent = useCallback((agent: string) => {
    const landed = readSeen(agent)
    return () => {
      landed()
      leaveReply()
    }
  }, [leaveReply, readSeen])
  const setPeekWidth = useCallback((width: number) => setPrefs((current) => ({ ...current, peek: clampPeekWidth(width) })), [])

  const select = useCallback((id: string) => {
    setSelected(id)
    setRevealTick((tick) => tick + 1)
  }, [setSelected])
  // by is the side a zoom from another zoom swipes in from.
  const zoomCard = useCallback((id: string, by: -1 | 1 = 1) => {
    setSelected(id)
    return zoomInto(id, entry(id), true, by)
  }, [entry, setSelected, zoomInto])

  // A step to a space in lens order from the zoomed one, wrapping: ⇧⌥←/→.
  const stepSpace = useCallback((by: -1 | 1) => {
    if (view.kind !== 'zoom') return false
    const next = spaceStep(order, activeSpaceID, by)
    return next ? zoomCard(next, by) : false
  }, [activeSpaceID, order, view.kind, zoomCard])

  const latest = useRef<LensKeyActions | null>(null)
  const lensKey = (action: (id: string) => void) => () => {
    if (!onLens || !chosen) return false
    action(chosen)
    setRevealTick((tick) => tick + 1)
    return true
  }
  latest.current = {
    step: (by) => lensKey((id) => setSelected(stepCard(order, id, by)))(),
    stepRow: (by) => lensKey((id) => setSelected(stepLine(cardRects(), id, by) ?? stepRow(spaces, prefs, id, by)))(),
    zoomIn: () => onLens && chosen ? zoomCard(chosen) : false,
    zoomOut: () => !enabled ? false : view.kind !== 'lens' ? zoomOut() : leavePeek(),
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
    toggleRead: () => lensKey((id) => readToggle(needsYou(attention[id] ?? quiet)) === 'read' ? markSpaceRead(id) : markSpaceUnread(id))(),
    markUnread: () => lensKey(markSpaceUnread)(),
    cycleText: () => lensKey(() => setPrefs((current) => ({ ...current, text: nextCardText(current.text ?? 'answer') })))(),
    cycleSize: () => lensKey(() => setPrefs((current) => ({ ...current, size: nextCardSize(current.size ?? 0, focusSizes.length) })))(),
  }
  useEffect(() => bindLensShortcuts(window, {
    step: (by) => latest.current?.step(by),
    stepRow: (by) => latest.current?.stepRow(by),
    zoomIn: () => latest.current?.zoomIn(),
    zoomOut: () => latest.current?.zoomOut(),
    nextNeeding: (zoom) => latest.current?.nextNeeding(zoom),
    place: (row) => latest.current?.place(row),
    cycleVisible: () => latest.current?.cycleVisible(),
    toggleRead: () => latest.current?.toggleRead(),
    markUnread: () => latest.current?.markUnread(),
    cycleText: () => latest.current?.cycleText(),
    cycleSize: () => latest.current?.cycleSize(),
  }), [])

  // A zoom's agent opened from a mention while alone: the first space in lens
  // order holding it, else alone again.
  const summon = useCallback((agent: string) => {
    const home = order.find((id) => (membersBySpace[id] ?? []).some((member) => member.kind === 'agent' && member.name === agent))
    if (home) zoomInto(home, agent)
    else openAlone(agent)
  }, [membersBySpace, openAlone, order, zoomInto])

  return { prefs, order, rows, selected: chosen, select, zoomCard, stepSpace, summon, revealTick, needYou: needsYou(total), peek, peekWidth: prefs.peek ?? peekWidth, setPeekWidth,
    reply: replying ? { agent: replying.agent, focus: replying.focus } : null, startReply, replySent, leaveReply }
}
