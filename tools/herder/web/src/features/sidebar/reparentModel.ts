import type { SidebarNode } from './sidebarNodes.ts'

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

export function dropAssignment(view: 'placement' | 'supervision', sourceID: string | null, targetID: string | null, nodes: Map<string, SidebarNode>, submit: (name: string, assignment: { manager: string }) => void) {
  const result = reparentDrop(view, sourceID, targetID, nodes)
  if ('refusal' in result) return false
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
