import type { FleetView } from '../layout/shellPreferences.ts'
import { buildSidebarNodes, buildSupervisionNodes, type SidebarNode } from './sidebarNodes.ts'

// nodeBuilder picks the tree for a view; both views share one row component
// and one expandedItems array (their node ids never collide).
export function nodeBuilder(view: FleetView) {
  return view === 'placement' ? buildSidebarNodes : buildSupervisionNodes
}

export const agentKinds = new Set<SidebarNode['kind']>(['pane', 'subagent', 'agent'])
const groupKinds = new Set<SidebarNode['kind']>(['workspace', 'unplaced', 'operator', 'tombstone', 'unadopted', 'unknown-manager', 'terminals', 'terminals-workspace'])

// defaultExpanded opens every group and every agent that has children; the
// first paint of either view starts fully open.
export function defaultExpanded(nodes: Map<string, SidebarNode>) {
  return [...nodes.values()].filter((node) => groupKinds.has(node.kind) || (agentKinds.has(node.kind) && node.children.length > 0)).map((node) => node.id)
}

// managerItems are the supervision nodes that fold: unseen ones open expanded,
// the same rule the placement view applies to unseen workspaces.
export function managerItems(nodes: Map<string, SidebarNode>) {
  return [...nodes.values()].filter((node) => (node.kind === 'agent' || node.kind === 'tombstone' || node.kind === 'unknown-manager') && node.children.length > 0).map((node) => node.id)
}
