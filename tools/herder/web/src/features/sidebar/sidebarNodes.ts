import { workspaceName } from '../../shared/workspaceName.ts'
import { screenPanePresentation } from '../screen/screenPresentation.ts'
import type { Board, Pane, Row, Workspace } from '../../types.ts'

export type SidebarNodeKind =
  | 'root' | 'workspace' | 'pane' | 'subagent' | 'unplaced'
  | 'agent' | 'tombstone' | 'terminals' | 'terminals-workspace'

export type SidebarNode = {
  id: string
  kind: SidebarNodeKind
  name: string
  children: string[]
  count?: number
  pane?: Pane | Row
  workspace?: Workspace
  tabLabel?: string
  // Secondary is shared identity/state; statusText is placement-only, while
  // summary is the folded supervision total.
  secondary?: string
  statusText?: string
  contextUsed?: number
  workspaceLabel?: string
  summary?: SupervisionSummary
  marker?: 'unknown-manager'
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
    ...(pane.agent !== '-' ? agentLabel(pane) : { name: screenPanePresentation(pane as Pane).label }),
    children,
    pane,
    tabLabel,
    statusText: pane.agent !== '-' && pane.bus_status !== '-' ? pane.bus_status : undefined,
    contextUsed: pane.context_used,
  })
  for (const [index, child] of (pane.subagents ?? []).entries()) addAgentNode(result, children[index], child)
}

export function agentLabel(row: Pick<Row, 'agent' | 'title'>) {
  return { name: row.title || row.agent, secondary: row.title ? row.agent : undefined }
}

// Supervision view: the tree is the manager edge, never placement. Every
// Row on the board (placed panes, unplaced rows, nested subagents) becomes
// one `agent:<full-name>` node; a row hangs under its manager, a Task
// subagent under its parent_agent exactly as in the placement view. Human- or
// unknown-managed rows sit at the top level beside tombstones for ended
// managers with live reports. A manager string that names no live row also
// leaves its row at the top level. Panes without a bus row are Terminals,
// grouped by workspace and always last. Siblings sort by roster created_at,
// oldest first, then name; status is never a sort key.
export type SupervisionSummary = { total: number, active: number }

const terminalsID = 'terminals'

type FlatRow = { row: Row, workspaceLabel?: string, tabLabel?: string }

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
      collect(pane, { workspaceLabel: label, tabLabel })
    }))
  })
  board.unplaced.forEach((row) => collect(row, {}))

  const reportsOf = new Map<string, FlatRow[]>()
  const tombstones = new Map<string, FlatRow[]>()
  const topLevel: FlatRow[] = []
  for (const flat of rows.values()) {
    const { row } = flat
    if (row.parent_agent && rows.has(row.parent_agent)) continue
    const manager = row.manager ?? ''
    switch (row.manager_state) {
      case 'operator':
        topLevel.push(flat)
        break
      case 'live':
        if (manager && rows.has(manager) && manager !== row.agent) push(reportsOf, manager, flat)
        else topLevel.push(flat)
        break
      case 'ended':
        if (manager) push(tombstones, manager, flat)
        else topLevel.push(flat)
        break
      default:
        topLevel.push(flat)
    }
  }

  // placed guards recursion: a row is added once, so a reparent cycle can
  // never loop; the final unreached-row pass below homes anything a cycle hid.
  const placed = new Set<string>()
  const addAgent = (flat: FlatRow): string => {
    const { row } = flat
    const id = agentNodeID(row.agent)
    placed.add(row.agent)
    const children: string[] = []
    for (const report of byCreation(reportsOf.get(row.agent) ?? [])) {
      if (placed.has(report.row.agent)) continue
      children.push(addAgent(report))
    }
    for (const child of row.subagents ?? []) {
      const flatChild = rows.get(child.agent)
      if (!flatChild || placed.has(child.agent)) continue
      children.push(addAgent(flatChild))
    }
    result.set(id, {
      id, kind: row.parent_agent ? 'subagent' : 'agent', ...agentLabel(row), children, pane: row,
      workspaceLabel: flat.workspaceLabel, tabLabel: flat.tabLabel, summary: summarise(result, children),
      contextUsed: row.context_used,
      ...(row.manager_state === 'unknown' ? { marker: 'unknown-manager' as const } : {}),
    })
    return id
  }

  const rootEntries: { id: string, createdAt: string, name: string }[] = []
  const rootRow = (flat: FlatRow) => rootEntries.push({ id: addAgent(flat), createdAt: flat.row.created_at ?? '', name: flat.row.agent })
  for (const flat of byCreation(topLevel)) {
    if (placed.has(flat.row.agent)) continue
    rootRow(flat)
  }
  for (const [manager, reports] of tombstones) {
    const id = `tombstone:${manager}`
    const orderedReports = byCreation(reports)
    const children = orderedReports.filter((flat) => !placed.has(flat.row.agent)).map((flat) => addAgent(flat))
    result.set(id, { id, kind: 'tombstone', name: manager, children, secondary: 'ended', summary: summarise(result, children) })
    rootEntries.push({ id, createdAt: orderedReports[0].row.created_at ?? '', name: manager })
  }
  // A live report whose manager subtree was never reached (its manager sits
  // under a cycle or was itself skipped) still needs a home.
  for (const flat of byCreation([...rows.values()])) {
    if (placed.has(flat.row.agent) || (flat.row.parent_agent && rows.has(flat.row.parent_agent) && placed.has(flat.row.parent_agent))) continue
    rootRow(flat)
  }
  root.children.push(...rootEntries
    .sort(creationOrder)
    .map((entry) => entry.id))

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

// expandedLabel is the row text in either state: identity plus its
// secondary (a bus name under a title or "ended" on a tombstone), joined by
// the ruled separator.
export function expandedLabel(node: SidebarNode) {
  return node.secondary ? `${node.name} · ${node.secondary}` : node.name
}

// collapsedLabel is what a folded subtree reads: the same identity and state
// text plus the descendant summary, `ziru (4)` or
// `fimu · ended (3 · 1 active)`. Nothing is dropped when folding.
export function collapsedLabel(node: SidebarNode) {
  const label = expandedLabel(node)
  if (!node.summary || node.summary.total === 0) return label
  if (node.kind === 'agent' || node.kind === 'subagent') return `${label} (${node.summary.total})`
  return `${label} (${node.summary.total} · ${node.summary.active} active)`
}

function push<T>(map: Map<string, T[]>, key: string, value: T) {
  const list = map.get(key)
  if (list) list.push(value)
  else map.set(key, [value])
}

function byCreation(flats: FlatRow[]) {
  return [...flats].sort((left, right) => creationOrder(
    { createdAt: left.row.created_at ?? '', name: left.row.agent },
    { createdAt: right.row.created_at ?? '', name: right.row.agent },
  ))
}

function creationOrder(left: { createdAt: string, name: string }, right: { createdAt: string, name: string }) {
  return left.createdAt.localeCompare(right.createdAt) || left.name.localeCompare(right.name)
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
    const descendants = child.summary ?? summarise(result, child.children)
    total += descendants.total
    active += descendants.active
  }
  return { total, active }
}
