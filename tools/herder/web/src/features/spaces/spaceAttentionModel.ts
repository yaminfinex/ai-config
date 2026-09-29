import type { Board, Pane, Row } from '../../types.ts'
import { findAgentRow } from '../../shared/agentStatus.ts'
import { panelParams, readStoredSpaceLayout } from '../layout/dockLayout.ts'
import { setMarkers, type ReadMarkers } from './readMarkerStore.ts'

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

// turnFingerprint is the turn-end signal: while an agent is listening, its
// session plus the context tokens herder folds from the transcript's
// last API call. Every finished turn moves context_used, a new session
// resets it, and both survive the browser closing. No fingerprint (not
// listening, or no vitals yet) means nothing can be unread.
export function turnFingerprint(row: Row | undefined): string | null {
  if (!row || row.bus_status !== 'listening' || typeof row.context_used !== 'number') return null
  return `${(row as Pane).agent_session ?? ''}:${row.context_used}`
}

export function agentAttention(row: Row | undefined, marker: string | undefined): 'blocked' | 'unread' | null {
  if (row?.bus_status === 'blocked') return 'blocked'
  const fingerprint = turnFingerprint(row)
  return fingerprint !== null && marker !== undefined && fingerprint !== marker ? 'unread' : null
}

export function spaceAttention(board: Board | undefined, agents: readonly string[], markers: ReadMarkers): SpaceAttention {
  const result: SpaceAttention = { unread: [], blocked: [] }
  for (const name of agents) {
    const state = agentAttention(findAgentRow(board, name), markers[name])
    if (state) result[state].push(name)
  }
  return result
}

// seedReadMarkers gives every open agent the board has never marked a
// marker at first sight, so a panel only turns unread for a turn that ends
// after it was in the layout. It waits for a board: seeding blind would
// record "no turn yet" and light up every agent when the board arrives.
export function seedReadMarkers(markers: ReadMarkers, board: Board | undefined, agents: readonly string[]): ReadMarkers {
  if (!board) return markers
  const updates: Record<string, string> = {}
  for (const name of agents) {
    if (markers[name] !== undefined) continue
    const row = findAgentRow(board, name)
    if (row) updates[name] = turnFingerprint(row) ?? ''
  }
  return setMarkers(markers, updates)
}

// markViewedRead records the current turn of each agent the owner has been
// looking at for the dwell. An agent mid-turn keeps its old marker so the
// turn it is running still lands as unread if the owner looks away first.
export function markViewedRead(markers: ReadMarkers, board: Board | undefined, viewed: readonly string[]): ReadMarkers {
  const updates: Record<string, string> = {}
  for (const name of viewed) {
    const fingerprint = turnFingerprint(findAgentRow(board, name))
    if (fingerprint !== null) updates[name] = fingerprint
  }
  return setMarkers(markers, updates)
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
