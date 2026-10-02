import type { Board, Row } from '../../types.ts'
import { findAgentRow } from '../../shared/agentStatus.ts'
import { panelParams, readStoredSpaceLayout } from '../layout/dockLayout.ts'
import { baselineMarker, type ReadMarker, type ReadMarkers, type ReadPosition } from './readMarkerModel.ts'
import { readThrough } from './readPositionModel.ts'

export type SpaceAttention = { unread: string[], blocked: string[] }
export const quietAttention: SpaceAttention = { unread: [], blocked: [] }

type StoredDock = { panels?: Record<string, { params?: unknown }> } | null | undefined

// agentsInDock names the agents with a transcript panel open in a
// serialized dock, in panel order, once each.
export function agentsInDock(dock: StoredDock): string[] {
  const names = Object.values(dock?.panels ?? {}).flatMap((panel) => {
    const params = panelParams(panel?.params)
    return params?.kind === 'agent' ? [params.name] : []
  })
  return [...new Set(names)]
}

// storedSpaceAgents reads another space's saved layout without the
// recovery write readStoredSpaceLayout performs when handed a setItem.
export function storedSpaceAgents(storage: Pick<Storage, 'getItem'>, spaceID: string): string[] {
  try {
    return agentsInDock(readStoredSpaceLayout({ getItem: (key) => storage.getItem(key) }, spaceID).stored?.dock)
  } catch {
    return []
  }
}

// turnEnd is the turn-end signal: the id of the agent's latest completed
// turn, stamped by the serve on placed panes and unplaced rows alike. No id
// means nothing can be unread; a retired or stopped agent never counts.
export function turnEnd(row: Row | undefined): number | null {
  if (!row || row.bus_status === 'retired' || row.bus_status === 'stopped') return null
  return typeof row.turn_end_id === 'number' && row.turn_end_id > 0 ? row.turn_end_id : null
}

// Only a listening agent, or one already on its next turn, can be waiting
// on a finished turn; inactive, pending, unknown or '-' never count.
const unreadStatuses = new Set(['listening', 'active'])

// agentAttention: blocked always shows; a deliberate mark unread is quiet
// unread; otherwise unread is a turn that ended after the marker's. With no
// marker the baseline is unknown, so nothing is unread until seeding
// records one.
export function agentAttention(row: Row | undefined, marker: ReadMarker | undefined): 'blocked' | 'unread' | null {
  if (row?.bus_status === 'blocked') return 'blocked'
  if (marker?.unread) return 'unread'
  if (!row || !unreadStatuses.has(row.bus_status)) return null
  const id = turnEnd(row)
  return id !== null && marker !== undefined && id > marker.turn ? 'unread' : null
}

export function spaceAttention(board: Board | undefined, agents: readonly string[], markers: ReadMarkers): SpaceAttention {
  const result: SpaceAttention = { unread: [], blocked: [] }
  for (const name of agents) {
    const state = agentAttention(findAgentRow(board, name), markers[name])
    if (state) result[state].push(name)
  }
  return result
}

// seedUpdates are the silent baselines for every open agent with no
// marker: one never seen, or one whose first turn_end_id arrives late (an
// unknown baseline is not a new completion). A panel then turns unread only
// for a turn that ends after that. They are written weakly.
export function seedUpdates(markers: ReadMarkers, board: Board | undefined, agents: readonly string[]): Record<string, ReadMarker> {
  const updates: Record<string, ReadMarker> = {}
  for (const name of agents) {
    if (markers[name] !== undefined) continue
    const id = turnEnd(findAgentRow(board, name))
    if (id !== null) updates[name] = baselineMarker(id)
  }
  return updates
}

// readUpdates are what the dwell writes for each agent the owner has been
// looking at: the latest turn end and the newest rendered entry (positions,
// by agent; undefined while its transcript end is still loading, null when
// it has none). A turn still running has no id yet, so it lands as unread
// if the owner looks away before it ends. A mark unread is held until
// armed (the agent was left and came back).
export function readUpdates({ markers, board, viewed, positions, armed, now }: {
  markers: ReadMarkers
  board: Board | undefined
  viewed: readonly string[]
  positions: Readonly<Record<string, ReadPosition | null | undefined>>
  armed: ReadonlySet<string>
  now: number
}): Record<string, ReadMarker> {
  const updates: Record<string, ReadMarker> = {}
  for (const name of viewed) {
    const marker = markers[name]
    if (marker?.unread && !armed.has(name)) continue
    const latest = positions[name]
    if (latest === undefined) continue
    const next = readThrough(marker, turnEnd(findAgentRow(board, name)), latest, now)
    if (next) updates[name] = next
  }
  return updates
}

// boardAgents names every agent row on the board, placed, unplaced or
// subagent.
export function boardAgents(board: Board): Set<string> {
  const names = new Set<string>()
  const visit = (row: Row) => {
    if (row.agent) names.add(row.agent)
    for (const child of row.subagents ?? []) visit(child)
  }
  for (const workspace of board.workspaces) for (const tab of workspace.tabs) for (const pane of tab.panes) visit(pane)
  for (const row of board.unplaced) visit(row)
  return names
}

// markerKeepSet names the markers this browser keeps: agents open in any
// space or on the board. null without a board: nothing is known gone.
// Pruning is local; the server's own sweep forgets long-absent agents.
export function markerKeepSet(board: Board | undefined, openAgents: readonly string[]): Set<string> | null {
  if (!board) return null
  const keep = boardAgents(board)
  for (const name of openAgents) keep.add(name)
  return keep
}

export function attentionLabel(attention: SpaceAttention): string {
  const unread = attention.unread.length
  const blocked = attention.blocked.length
  const agents = (count: number) => `${count} agent${count === 1 ? '' : 's'}`
  if (unread && blocked) return `${agents(unread)} waiting, ${blocked} blocked`
  if (unread) return `${agents(unread)} waiting`
  if (blocked) return `${agents(blocked)} blocked`
  return 'no agents waiting'
}

export function totalAttention(values: readonly SpaceAttention[]): SpaceAttention {
  return {
    unread: [...new Set(values.flatMap((value) => value.unread))],
    blocked: [...new Set(values.flatMap((value) => value.blocked))],
  }
}
