import { workspaceName } from '../../shared/workspaceName.ts'
import { screenPanePresentation } from '../screen/screenPresentation.ts'
import type { Board, Pane, Row } from '../../types.ts'

export type SidebarNode = {
  id: string
  kind: 'root' | 'workspace' | 'pane' | 'subagent' | 'unplaced'
  name: string
  children: string[]
  count?: number
  pane?: Pane | Row
  tabLabel?: string
}

export function buildSidebarNodes(board: Board | undefined): Map<string, SidebarNode> {
  const result = new Map<string, SidebarNode>()
  const root: SidebarNode = { id: 'tree-root', kind: 'root', name: 'Fleet', children: [] }
  result.set(root.id, root)
  if (!board) return result

  const workspaces = new Map(board.workspaces.map((workspace) => [workspace.workspace_id, workspace]))
  const workspaceChildren = new Map<string, string[]>()
  board.workspaces.forEach((workspace) => workspaceChildren.set(workspace.workspace_id, []))
  board.workspaces.forEach((workspace) => {
    const id = `workspace:${workspace.workspace_id}`
    if (workspace.worktree_of && workspaces.has(workspace.worktree_of)) workspaceChildren.get(workspace.worktree_of)?.push(id)
    else root.children.push(id)
  })
  board.workspaces.forEach((workspace) => {
    const id = `workspace:${workspace.workspace_id}`
    const children: string[] = []
    const panes = workspace.tabs.flatMap((tab) => tab.panes.map((pane) => ({ pane, tab })))
      .sort((left, right) => Number(left.pane.agent === '-') - Number(right.pane.agent === '-'))
    panes.forEach(({ pane, tab }) => {
      const paneID = `pane:${pane.pane_id}`
      children.push(paneID)
      addAgentNode(result, paneID, pane, `tab ${tab.number}: ${tab.label || tab.tab_id}`)
    })
    children.push(...(workspaceChildren.get(workspace.workspace_id) ?? []))
    result.set(id, { id, kind: 'workspace', name: workspaceName(workspace.label, workspace.workspace_id), children, count: workspace.pane_count })
  })
  const unplaced: SidebarNode = { id: 'unplaced', kind: 'unplaced', name: 'Unplaced', children: [], count: board.unplaced.length }
  board.unplaced.forEach((row) => {
    const id = `unplaced:${row.agent}`
    unplaced.children.push(id)
    addAgentNode(result, id, row)
  })
  root.children.push(unplaced.id)
  result.set(unplaced.id, unplaced)
  return result
}

function addAgentNode(result: Map<string, SidebarNode>, id: string, pane: Pane | Row, tabLabel?: string) {
  const children = (pane.subagents ?? []).map((child) => `${id}:subagent:${child.agent}`)
  result.set(id, {
    id,
    kind: pane.parent_agent ? 'subagent' : 'pane',
    name: pane.agent !== '-' ? pane.agent : screenPanePresentation(pane as Pane).label,
    children,
    pane,
    tabLabel,
  })
  for (const [index, child] of (pane.subagents ?? []).entries()) addAgentNode(result, children[index], child)
}
