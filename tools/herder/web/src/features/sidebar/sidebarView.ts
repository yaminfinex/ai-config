import type { SidebarNode } from './sidebarNodes.ts'

export const agentKinds = new Set<SidebarNode['kind']>(['pane', 'subagent', 'agent'])
const groupKinds = new Set<SidebarNode['kind']>(['workspace', 'unplaced', 'tombstone', 'terminals', 'terminals-workspace'])

// defaultExpanded opens every group and every agent that has children; the
// first paint of either view starts fully open.
export function defaultExpanded(nodes: Map<string, SidebarNode>) {
  return [...nodes.values()].filter((node) => groupKinds.has(node.kind) || (agentKinds.has(node.kind) && node.children.length > 0)).map((node) => node.id)
}

// managerItems are the supervision nodes that fold: unseen ones open expanded,
// the same rule the placement view applies to unseen workspaces.
export function managerItems(nodes: Map<string, SidebarNode>) {
  return [...nodes.values()].filter((node) => (node.kind === 'agent' || node.kind === 'tombstone') && node.children.length > 0).map((node) => node.id)
}

export type ExpansionState = {
  expandedItems: string[] | null
  knownWorkspaceItems: string[] | null
  knownManagerItems: string[] | null
}

// reconcileExpansion is the only transition the sidebar applies to its tree
// state, and it runs on the board, never on the view: the first board opens
// both trees fully; a browser that already had placement state opens the
// supervision groups once; afterwards only never-seen workspaces and manager
// subtrees are added. Switching views is not an input, so it can never
// reset or shrink expandedItems. Null means nothing to apply.
export function reconcileExpansion(placementNodes: Map<string, SidebarNode>, supervisionNodes: Map<string, SidebarNode>, state: ExpansionState): Partial<ExpansionState> | null {
  const workspaceItems = [...placementNodes.values()].filter((node) => node.kind === 'workspace').map((node) => node.id)
  const managers = managerItems(supervisionNodes)
  if (state.expandedItems === null) {
    return {
      expandedItems: [...new Set([...defaultExpanded(placementNodes), ...defaultExpanded(supervisionNodes)])],
      knownWorkspaceItems: workspaceItems,
      knownManagerItems: managers,
    }
  }
  if (state.knownWorkspaceItems === null) return { knownWorkspaceItems: workspaceItems }
  if (state.knownManagerItems === null) {
    return { expandedItems: [...new Set([...state.expandedItems, ...defaultExpanded(supervisionNodes)])], knownManagerItems: managers }
  }
  const known = new Set([...state.knownWorkspaceItems, ...state.knownManagerItems])
  const unseen = [...workspaceItems, ...managers].filter((id) => !known.has(id))
  if (unseen.length === 0) return null
  return {
    expandedItems: [...new Set([...state.expandedItems, ...unseen])],
    knownWorkspaceItems: [...new Set([...state.knownWorkspaceItems, ...workspaceItems])],
    knownManagerItems: [...new Set([...state.knownManagerItems, ...managers])],
  }
}
