import type { FleetView } from '../layout/shellPreferences.ts'
import type { SidebarNode } from './sidebarNodes.ts'
import { reparentDrop } from './reparentModel.ts'
import type { AssignmentPatch } from '../../api/client.ts'

// Groups view drag: an agent row dropped on a group header sets that agent's
// group to the header's label; the Ungrouped header (label '') clears it. The
// plan is null in every other case, so nothing else on the tree is a drop
// target: the supervision and placement views, agent rows, workspaces.
export type GroupDrop = { name: string, group: string }

export function planGroupDrop(view: FleetView, source: string | null | undefined, target: SidebarNode | undefined): GroupDrop | null {
  if (view !== 'groups' || !source || source === '-' || !target) return null
  if (target.kind !== 'group' && target.kind !== 'ungrouped') return null
  return { name: source, group: target.group ?? '' }
}

export function groupHeaderTooltip(node: SidebarNode, members: number) {
  const seats = `${members} ${members === 1 ? 'agent' : 'agents'}`
  return node.kind === 'ungrouped'
    ? `Ungrouped · ${seats} · drop an agent here to clear its group`
    : `group ${node.name} · ${seats} · drop an agent here to set its group`
}

export function openGroupTooltip(name: string, members: number) {
  return `Open group ${name} as a space (open ${members} ${members === 1 ? 'transcript' : 'transcripts'})`
}

// Open-as-space: the ruling is "creates or refreshes" — an existing space
// named exactly after the group is switched to, else one is created with
// that name; then every member not yet open there is opened pinned. Nothing
// is closed and no link between group and space is stored.
export type OpenGroupPlan = { action: 'switch', id: string } | { action: 'create', name: string }

export function planOpenGroupAsSpace(name: string, spaces: { id: string, name: string }[]): OpenGroupPlan {
  const existing = spaces.find((space) => space.name === name)
  return existing ? { action: 'switch', id: existing.id } : { action: 'create', name }
}

export function membersToOpen(members: string[], alreadyOpen: Iterable<string>) {
  const open = new Set(alreadyOpen)
  return members.filter((member) => !open.has(member))
}

// planSidebarDrop is the ONE drop seam the sidebar calls: node ids in, one
// assignment out. The plan is chosen by view — groups → a header sets or
// clears the group (planGroupDrop), supervision → a row or the empty top
// level sets the manager (reparentDrop) — so every row carries one set of
// drag handlers whatever view is showing. Null means the drop is refused.
export type SidebarDrop = { name: string, assignment: AssignmentPatch }

export function planSidebarDrop(view: FleetView, sourceID: string, targetID: string | null, nodes: Map<string, SidebarNode>): SidebarDrop | null {
  if (view === 'groups') {
    const plan = planGroupDrop(view, nodes.get(sourceID)?.pane?.agent, targetID === null ? undefined : nodes.get(targetID))
    return plan ? { name: plan.name, assignment: { group: plan.group } } : null
  }
  const result = reparentDrop(view, sourceID, targetID, nodes)
  return 'refusal' in result ? null : result
}
