import { workspaceName } from '../../shared/workspaceName.ts'
import { screenPanePresentation } from '../screen/screenPresentation.ts'
import type { Board, Pane, Row, Workspace } from '../../types.ts'

export type SidebarNodeKind =
  | 'root' | 'workspace' | 'pane' | 'subagent' | 'unplaced'
  | 'operator' | 'agent' | 'tombstone' | 'unadopted' | 'unknown-manager' | 'terminals' | 'terminals-workspace'

export type SidebarNode = {
  id: string
  kind: SidebarNodeKind
  name: string
  children: string[]
  count?: number
  pane?: Pane | Row
  workspace?: Workspace
  tabLabel?: string
  // Supervision view only: bus name under a title (or "ended"/"unknown" on
  // group nodes), placement as a trailing chip, and the folded summary.
  secondary?: string
  paneChip?: string
  workspaceLabel?: string
  summary?: SupervisionSummary
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
    result.set(id, { id, kind: 'workspace', name: workspaceName(workspace.label, workspace.workspace_id), children, count: workspace.pane_count, workspace })
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

// Supervision view: the tree is the manager edge, never placement. Every
// Row on the board (placed panes, unplaced rows, nested subagents) becomes
// one `agent:<full-name>` node; a row hangs under its manager, a Task
// subagent under its parent_agent exactly as in the placement view. Roots:
// the operator ("you") holds manager_state operator rows and, last, a
// tombstone for every ended manager that still has live reports; unknown or
// empty managers land in Unadopted (a manager string that names no live row
// keeps its reports together as "<name> (unknown)"); panes without a bus row
// are Terminals grouped by workspace. Children sort by roster created_at,
// oldest first; status is never a sort key.
export type SupervisionSummary = { total: number, active: number }

const operatorID = 'operator'
const unadoptedID = 'unadopted'
const terminalsID = 'terminals'

type FlatRow = { row: Row, paneChip?: string, workspaceLabel?: string, tabLabel?: string }

export function agentNodeID(name: string) {
  return `agent:${name}`
}

export function buildSupervisionNodes(board: Board | undefined): Map<string, SidebarNode> {
  const result = new Map<string, SidebarNode>()
  const root: SidebarNode = { id: 'tree-root', kind: 'root', name: 'Fleet', children: [] }
  result.set(root.id, root)
  if (!board) return result

  const rows = new Map<string, FlatRow>()
  const terminals = new Map<string, { workspace: Workspace, panes: { pane: Pane, tabLabel: string }[] }>()
  const collect = (row: Row, context: Omit<FlatRow, 'row'>) => {
    if (row.agent === '-' || row.bus_status === '-') return
    if (!rows.has(row.agent)) rows.set(row.agent, { row, ...context })
    for (const child of row.subagents ?? []) collect(child, {})
  }
  board.workspaces.forEach((workspace) => {
    const label = workspaceName(workspace.label, workspace.workspace_id)
    workspace.tabs.forEach((tab) => tab.panes.forEach((pane) => {
      const tabLabel = `tab ${tab.number}: ${tab.label || tab.tab_id}`
      if (pane.bus_status === '-') {
        const entry = terminals.get(workspace.workspace_id) ?? { workspace, panes: [] }
        entry.panes.push({ pane, tabLabel })
        terminals.set(workspace.workspace_id, entry)
        return
      }
      collect(pane, { paneChip: pane.pane_id, workspaceLabel: label, tabLabel })
    }))
  })
  board.unplaced.forEach((row) => collect(row, {}))

  const reportsOf = new Map<string, FlatRow[]>()
  const tombstones = new Map<string, FlatRow[]>()
  const unadopted: FlatRow[] = []
  const unknownGroups = new Map<string, FlatRow[]>()
  const operatorReports: FlatRow[] = []
  for (const flat of rows.values()) {
    const { row } = flat
    if (row.parent_agent && rows.has(row.parent_agent)) continue
    const manager = row.manager ?? ''
    switch (row.manager_state) {
      case 'operator':
        operatorReports.push(flat)
        break
      case 'live':
        if (manager && rows.has(manager) && manager !== row.agent) push(reportsOf, manager, flat)
        else unadopted.push(flat)
        break
      case 'ended':
        if (manager) push(tombstones, manager, flat)
        else unadopted.push(flat)
        break
      default:
        if (manager) push(unknownGroups, manager, flat)
        else unadopted.push(flat)
    }
  }

  const placed = new Set<string>()
  const addAgent = (flat: FlatRow, ancestors: Set<string>): string => {
    const { row } = flat
    const id = agentNodeID(row.agent)
    placed.add(row.agent)
    const next = new Set(ancestors).add(row.agent)
    const children: string[] = []
    for (const report of byCreation(reportsOf.get(row.agent) ?? [])) {
      if (next.has(report.row.agent) || placed.has(report.row.agent)) continue
      children.push(addAgent(report, next))
    }
    for (const child of row.subagents ?? []) {
      const flatChild = rows.get(child.agent)
      if (!flatChild || next.has(child.agent) || placed.has(child.agent)) continue
      children.push(addAgent(flatChild, next))
    }
    result.set(id, {
      id, kind: row.parent_agent ? 'subagent' : 'agent', name: row.title || row.agent, children, pane: row,
      secondary: row.title ? row.agent : undefined, paneChip: flat.paneChip, workspaceLabel: flat.workspaceLabel, tabLabel: flat.tabLabel,
      summary: summarise(result, children),
    })
    return id
  }

  const operator: SidebarNode = { id: operatorID, kind: 'operator', name: 'you', children: [] }
  result.set(operator.id, operator)
  root.children.push(operator.id)
  for (const flat of byCreation(operatorReports)) operator.children.push(addAgent(flat, new Set()))
  for (const [manager, reports] of [...tombstones.entries()].sort(([left], [right]) => left.localeCompare(right))) {
    const id = `tombstone:${manager}`
    const children = byCreation(reports).filter((flat) => !placed.has(flat.row.agent)).map((flat) => addAgent(flat, new Set([manager])))
    result.set(id, { id, kind: 'tombstone', name: manager, children, secondary: 'ended', summary: summarise(result, children) })
    operator.children.push(id)
  }
  operator.summary = summarise(result, operator.children)

  const unadoptedNode: SidebarNode = { id: unadoptedID, kind: 'unadopted', name: 'Unadopted', children: [] }
  for (const flat of byCreation(unadopted)) {
    if (placed.has(flat.row.agent)) continue
    unadoptedNode.children.push(addAgent(flat, new Set()))
  }
  for (const [manager, reports] of [...unknownGroups.entries()].sort(([left], [right]) => left.localeCompare(right))) {
    const id = `unknown:${manager}`
    const children = byCreation(reports).filter((flat) => !placed.has(flat.row.agent)).map((flat) => addAgent(flat, new Set([manager])))
    if (children.length === 0) continue
    result.set(id, { id, kind: 'unknown-manager', name: manager, children, secondary: 'unknown', summary: summarise(result, children) })
    unadoptedNode.children.push(id)
  }
  // A live report whose manager subtree was never reached (its manager sits
  // under a cycle or was itself skipped) still needs a home.
  for (const flat of byCreation([...rows.values()])) {
    if (placed.has(flat.row.agent) || (flat.row.parent_agent && rows.has(flat.row.parent_agent) && placed.has(flat.row.parent_agent))) continue
    unadoptedNode.children.push(addAgent(flat, new Set()))
  }
  unadoptedNode.summary = summarise(result, unadoptedNode.children)
  unadoptedNode.count = unadoptedNode.summary.total
  result.set(unadoptedNode.id, unadoptedNode)
  root.children.push(unadoptedNode.id)

  const terminalsNode: SidebarNode = { id: terminalsID, kind: 'terminals', name: 'Terminals', children: [], count: 0 }
  for (const [workspaceID, entry] of terminals) {
    const id = `${terminalsID}:${workspaceID}`
    const children: string[] = []
    for (const { pane, tabLabel } of entry.panes) {
      const paneID = `pane:${pane.pane_id}`
      children.push(paneID)
      result.set(paneID, { id: paneID, kind: 'pane', name: pane.agent !== '-' ? pane.agent : screenPanePresentation(pane).label, children: [], pane, tabLabel })
    }
    result.set(id, { id, kind: 'terminals-workspace', name: workspaceName(entry.workspace.label, workspaceID), children, count: children.length, workspace: entry.workspace })
    terminalsNode.children.push(id)
    terminalsNode.count = (terminalsNode.count ?? 0) + children.length
  }
  result.set(terminalsNode.id, terminalsNode)
  root.children.push(terminalsNode.id)
  return result
}

// collapsedLabel is what a manager subtree reads when folded: `ziru (4 · 1 active)`.
export function collapsedLabel(node: SidebarNode) {
  if (!node.summary || node.summary.total === 0) return node.name
  return `${node.name} (${node.summary.total} · ${node.summary.active} active)`
}

function push<T>(map: Map<string, T[]>, key: string, value: T) {
  map.set(key, [...(map.get(key) ?? []), value])
}

function byCreation(flats: FlatRow[]) {
  return [...flats].sort((left, right) => (left.row.created_at ?? '').localeCompare(right.row.created_at ?? ''))
}

function summarise(result: Map<string, SidebarNode>, children: string[]): SupervisionSummary {
  let total = 0
  let active = 0
  for (const id of children) {
    const child = result.get(id)
    if (!child) continue
    if (child.pane && child.pane.agent !== '-') {
      total += 1
      if (child.pane.bus_status === 'active') active += 1
    }
    if (child.summary) {
      total += child.summary.total
      active += child.summary.active
    }
  }
  return { total, active }
}
