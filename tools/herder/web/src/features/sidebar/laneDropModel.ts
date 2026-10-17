import type { FleetView } from '../layout/shellPreferences.ts'
import type { SidebarNode } from './sidebarNodes.ts'

// Lanes view drag: an agent row dropped on a lane header OR on any row under
// it moves that agent into the lane; dropped on the pending "+ lane" header
// it creates the lane with that agent as lead (herder lane new). Every node
// under a lane carries `lane`, so no node id is parsed here. No lane, an
// agent's own row, and the other views refuse.
export type LaneDrop = { name: string, lane: string } | { name: string, newLane: string }

export function planLaneDrop(view: FleetView, source: string | null | undefined, target: SidebarNode | undefined): LaneDrop | null {
  if (view !== 'lanes' || !source || source === '-' || !target) return null
  if (target.placeholder) return { name: source, newLane: target.name }
  if (target.lane === undefined || target.pane?.agent === source) return null
  return { name: source, lane: target.lane }
}

export function laneHeaderTooltip(node: SidebarNode, members: number) {
  const seats = `${members} ${members === 1 ? 'agent' : 'agents'}`
  if (node.placeholder) return `new lane ${node.name} · drop an agent here to create it with that agent as lead`
  return node.kind === 'no-lane'
    ? `No lane · ${seats}`
    : `lane ${node.name} · ${node.secondary ?? 'no lead'} · ${seats} · drop an agent here to move it into the lane`
}

// The lane name rules mirror the serve's Validate (agentstore/event.go):
// trimmed, non-empty, at most 80 runes, no control characters, and not a
// name a lane on the board already has (case-insensitive, as herder lane).
export const maxLaneNameLength = 80

export type LaneNameCheck = { ok: true, name: string } | { ok: false, reason: string }

export function validateLaneName(raw: string, existing: Iterable<string>): LaneNameCheck {
  const name = raw.trim()
  if (!name) return { ok: false, reason: 'lane name is empty' }
  if (name.startsWith('-')) return { ok: false, reason: 'lane name must not start with -' }
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x1f\x7f]/.test(name)) return { ok: false, reason: 'lane name must not contain control characters' }
  if ([...name].length > maxLaneNameLength) return { ok: false, reason: `lane name must not exceed ${maxLaneNameLength} characters` }
  for (const label of existing) if (label.toLowerCase() === name.toLowerCase()) return { ok: false, reason: `lane ${label} already exists` }
  return { ok: true, name }
}
