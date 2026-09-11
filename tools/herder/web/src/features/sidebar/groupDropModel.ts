import type { FleetView } from '../layout/shellPreferences.ts'
import type { SidebarNode } from './sidebarNodes.ts'

// Groups view drag: an agent row dropped on a group header OR on any row under
// it sets that agent's group to the header's label; the Ungrouped header and
// its rows (label '') clear it. Every groups-view node carries `group` (the
// label a drop on it writes), so no node id is parsed here. An agent dropped
// on its own row is refused, as is anything without a group field: the
// supervision and placement views, workspaces.
export type GroupDrop = { name: string, group: string }

export function planGroupDrop(view: FleetView, source: string | null | undefined, target: SidebarNode | undefined): GroupDrop | null {
  if (view !== 'groups' || !source || source === '-' || !target || target.group === undefined) return null
  if (target.pane?.agent === source) return null
  return { name: source, group: target.group }
}

export function groupHeaderTooltip(node: SidebarNode, members: number) {
  const seats = `${members} ${members === 1 ? 'agent' : 'agents'}`
  return node.kind === 'ungrouped'
    ? `Ungrouped · ${seats} · drop an agent here to clear its group`
    : `group ${node.name} · ${seats} · drop an agent here to set its group`
}

export function openGroupTooltip(name: string, members: number) {
  return `Open space ${name} (creates it with ${members} pinned ${members === 1 ? 'transcript' : 'transcripts'} the first time; matched by name, no saved link)`
}

// Open-as-space: the ruling is "creates once, then jumps" — an existing space
// named exactly after the group is switched to and left exactly as the
// operator configured it; only a space created here opens every member
// pinned. Nothing is closed and no link between group and space is stored.
export type OpenGroupPlan = { action: 'switch', id: string } | { action: 'create', name: string }

export function planOpenGroupAsSpace(name: string, spaces: { id: string, name: string }[]): OpenGroupPlan {
  const existing = spaces.find((space) => space.name === name)
  return existing ? { action: 'switch', id: existing.id } : { action: 'create', name }
}

// runOpenGroupAsSpace executes the plan: 'switch' jumps to the matched space
// (a no-op when it is already active) and opens NOTHING, so a member the
// operator closed stays closed; 'create' makes the space and opens every
// member pinned once. Returns false only when the space could not be reached.
export function runOpenGroupAsSpace(plan: OpenGroupPlan, members: string[], dependencies: {
  activeID: string | null
  switchTo: (id: string) => boolean
  createNamed: (name: string) => boolean
  open: (member: string) => void
}) {
  if (plan.action === 'switch') return dependencies.activeID === plan.id || dependencies.switchTo(plan.id)
  if (!dependencies.createNamed(plan.name)) return false
  members.forEach(dependencies.open)
  return true
}
