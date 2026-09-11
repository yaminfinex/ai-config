import type { SidebarNode } from './sidebarNodes.ts'
import type { FleetView } from '../layout/shellPreferences.ts'
import type { AssignmentPatch } from '../../api/client.ts'
import { planGroupDrop } from './groupDropModel.ts'

export type ReparentDrop = { name: string, assignment: { manager: string } }
export type DropRefusal = 'placement view' | 'source is not a live agent' | 'self' | 'descendant' | 'tombstone' | 'terminal' | 'target is not a live agent'

export function reparentDrop(view: 'placement' | 'supervision', sourceID: string | null, targetID: string | null, nodes: Map<string, SidebarNode>): ReparentDrop | { refusal: DropRefusal } {
  if (view !== 'supervision') return { refusal: 'placement view' }
  if (sourceID === null) return { refusal: 'source is not a live agent' }
  const source = nodes.get(sourceID)
  if (!source?.pane || source.pane.agent === '-' || source.pane.bus_status === '-') return { refusal: 'source is not a live agent' }
  if (targetID === null || targetID === 'tree-root') return { name: source.pane.agent, assignment: { manager: 'human' } }
  if (targetID === sourceID) return { refusal: 'self' }
  const target = nodes.get(targetID)
  if (!target) return { refusal: 'target is not a live agent' }
  if (target.kind === 'tombstone') return { refusal: 'tombstone' }
  if (target.kind === 'pane' && target.pane?.agent === '-') return { refusal: 'terminal' }
  if (!target.pane || target.pane.agent === '-' || target.pane.bus_status === '-' || target.pane.manager_state === 'ended') return { refusal: 'target is not a live agent' }
  if (descendants(sourceID, nodes).has(targetID)) return { refusal: 'descendant' }
  return { name: source.pane.agent, assignment: { manager: target.pane.agent } }
}

// planSidebarDrop is the ONE drop seam the sidebar calls: node ids in, one
// assignment out. The plan is chosen by view — groups → a header or any row
// under it sets or clears the group (planGroupDrop), supervision → a row or the empty top
// level sets the manager (reparentDrop) — so every row carries one set of
// drag handlers whatever view is showing. Null means the drop is refused.
export type SidebarDrop = { name: string, assignment: AssignmentPatch }

export function planSidebarDrop(view: FleetView, sourceID: string | null, targetID: string | null, nodes: Map<string, SidebarNode>): SidebarDrop | null {
  if (sourceID === null) return null
  if (view === 'groups') {
    const plan = planGroupDrop(view, nodes.get(sourceID)?.pane?.agent, targetID === null ? undefined : nodes.get(targetID))
    return plan ? { name: plan.name, assignment: { group: plan.group } } : null
  }
  const result = reparentDrop(view, sourceID, targetID, nodes)
  return 'refusal' in result ? null : result
}

// dropAssignment is what both drop handlers (row and container) call: one
// plan through the seam, one submit when it is accepted, false on refusal.
export function dropAssignment(view: FleetView, sourceID: string | null, targetID: string | null, nodes: Map<string, SidebarNode>, submit: (name: string, assignment: AssignmentPatch) => void) {
  const result = planSidebarDrop(view, sourceID, targetID, nodes)
  if (!result) return false
  submit(result.name, result.assignment)
  return true
}

function descendants(sourceID: string, nodes: Map<string, SidebarNode>) {
  const found = new Set<string>()
  const visit = (id: string) => {
    for (const child of nodes.get(id)?.children ?? []) {
      if (found.has(child)) continue
      found.add(child)
      visit(child)
    }
  }
  visit(sourceID)
  return found
}
