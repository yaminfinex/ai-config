import { useEffect, useMemo, useState } from 'react'
import { hotkeysCoreFeature, selectionFeature, syncDataLoaderFeature } from '@headless-tree/core'
import { useTree } from '@headless-tree/react'
import { AgentStatusDot, gapLabel, workspaceName } from '../../shared/presentation'
import type { Board, Pane, Row } from '../../types'

const emptyExpandedItems: string[] = []

type SidebarNode = {
  id: string
  kind: 'root' | 'workspace' | 'pane' | 'unplaced'
  name: string
  children: string[]
  count?: number
  pane?: Pane | Row
  tabLabel?: string
}

export function FleetSidebar({ board, activeAgent, onOpenAgent, expandedItems, onExpandedItems, knownWorkspaceItems, onKnownWorkspaceItems }: {
  board: Board | undefined
  activeAgent?: string
  onOpenAgent: (name: string) => void
  expandedItems: string[] | null
  onExpandedItems: (items: string[]) => void
  knownWorkspaceItems: string[] | null
  onKnownWorkspaceItems: (items: string[]) => void
}) {
  const [selectedItems, setSelectedItems] = useState<string[]>([])
  const nodes = useMemo(() => {
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
      workspace.tabs.forEach((tab) => tab.panes.forEach((pane) => {
        const paneID = `pane:${pane.pane_id}`
        children.push(paneID)
        result.set(paneID, {
          id: paneID,
          kind: 'pane',
          name: pane.agent !== '-' ? pane.agent : pane.label || pane.pane_id,
          children: [],
          pane,
          tabLabel: `tab ${tab.number}: ${tab.label || tab.tab_id}`,
        })
      }))
      children.push(...(workspaceChildren.get(workspace.workspace_id) ?? []))
      result.set(id, { id, kind: 'workspace', name: workspaceName(workspace.label, workspace.workspace_id), children, count: workspace.pane_count })
    })
    const unplaced: SidebarNode = { id: 'unplaced', kind: 'unplaced', name: 'Unplaced', children: [], count: board.unplaced.length }
    board.unplaced.forEach((row) => {
      const id = `unplaced:${row.agent}`
      unplaced.children.push(id)
      result.set(id, { id, kind: 'pane', name: row.agent, children: [], pane: row })
    })
    root.children.push(unplaced.id)
    result.set(unplaced.id, unplaced)
    return result
  }, [board])

  useEffect(() => {
    if (!board) return
    const workspaceItems = [...nodes.values()].filter((node) => node.kind === 'workspace').map((node) => node.id)
    if (expandedItems === null) {
      onExpandedItems([...nodes.values()].filter((node) => node.kind === 'workspace' || node.kind === 'unplaced').map((node) => node.id))
      onKnownWorkspaceItems(workspaceItems)
      return
    }
    if (knownWorkspaceItems === null) {
      onKnownWorkspaceItems(workspaceItems)
      return
    }
    const known = new Set(knownWorkspaceItems)
    const unseen = workspaceItems.filter((id) => !known.has(id))
    if (unseen.length === 0) return
    onExpandedItems([...new Set([...expandedItems, ...unseen])])
    onKnownWorkspaceItems([...new Set([...knownWorkspaceItems, ...workspaceItems])])
  }, [board, expandedItems, knownWorkspaceItems, nodes, onExpandedItems, onKnownWorkspaceItems])

  useEffect(() => {
    if (!activeAgent) {
      setSelectedItems([])
      return
    }
    const match = [...nodes.values()].find((node) => node.kind === 'pane' && node.pane?.agent === activeAgent)
    setSelectedItems(match ? [match.id] : [])
  }, [activeAgent, nodes])

  const tree = useTree<SidebarNode>({
    rootItemId: 'tree-root',
    getItemName: (item) => item.getItemData().name,
    isItemFolder: (item) => item.getItemData().children.length > 0,
    dataLoader: {
      getItem: (id) => nodes.get(id) ?? nodes.get('tree-root')!,
      getChildren: (id) => nodes.get(id)?.children ?? [],
    },
    state: { expandedItems: expandedItems ?? emptyExpandedItems, selectedItems },
    setExpandedItems: (update) => onExpandedItems(typeof update === 'function' ? update(expandedItems ?? emptyExpandedItems) : update),
    setSelectedItems: (update) => setSelectedItems((current) => typeof update === 'function' ? update(current) : update),
    onPrimaryAction: (item) => {
      const node = item.getItemData()
      if (node.pane?.agent && node.pane.agent !== '-') onOpenAgent(node.pane.agent)
    },
    hotkeys: {
      customPrimaryActionEnter: { hotkey: 'Enter', preventDefault: true, handler: (_event, currentTree) => currentTree.getFocusedItem()?.primaryAction() },
      customPrimaryActionSpace: { hotkey: 'Space', preventDefault: true, handler: (_event, currentTree) => currentTree.getFocusedItem()?.primaryAction() },
    },
    features: [syncDataLoaderFeature, selectionFeature, hotkeysCoreFeature],
  })

  useEffect(() => { tree.rebuildTree() }, [nodes, tree])

  return <aside className="fleet-sidebar" aria-label="Fleet sidebar">
    <div className="sidebar-heading"><span className="status-dot listening" /><strong>Fleet</strong><span>herdr truth</span></div>
    {!board ? <p className="sidebar-loading">Waiting for fleet…</p> : <div {...tree.getContainerProps('Workspaces and agents')} className="fleet-tree">
      {tree.getItems().map((item) => {
        const node = item.getItemData()
        const pane = node.pane
        const signal = pane && pane.agent !== '-' && pane.bus_status !== '-' ? pane.bus_status : ''
        const folder = item.isFolder()
        return <div
          {...item.getProps()}
          key={item.getId()}
          className={`tree-row ${node.kind === 'pane' ? 'pane-row' : 'workspace-row'}${pane?.agent && pane.agent !== '-' ? ' agent-row' : ''}${pane?.agent === '-' ? ' shell-row' : ''}${node.kind === 'unplaced' ? ' unplaced-row' : ''}${item.isFocused() ? ' tree-focused' : ''}${item.isSelected() ? ' selected' : ''}`}
          style={{ paddingLeft: `${item.getItemMeta().level * 16 + 5}px` }}
          title={pane ? `${pane.pane_id}${node.tabLabel ? ` · ${node.tabLabel}` : ''} · ${pane.tool} · herdr ${pane.herdr_status}${signal ? ` · bus ${signal}` : ''}` : node.name}
          onFocus={() => item.setFocused()}
        >
          {folder ? <button
            className={`disclosure${item.isExpanded() ? ' expanded' : ''}`}
            type="button"
            aria-label={`${item.isExpanded() ? 'Collapse' : 'Expand'} ${node.name}`}
            title={`${item.isExpanded() ? 'Collapse' : 'Expand'} ${node.name}`}
            onClick={(event) => { event.stopPropagation(); if (item.isExpanded()) item.collapse(); else item.expand() }}
          ><span aria-hidden="true">›</span></button> : <span className="disclosure-spacer" />}
          {pane?.agent && pane.agent !== '-' && <AgentStatusDot status={pane.bus_status} />}
          <span className="tree-name">{node.name}</span>
          {folder && <span className="count-badge">{node.count ?? node.children.length}</span>}
          {signal && <span className="bus-status">{signal}</span>}
          {pane && pane.agent !== '-' && pane.gap !== '-' && <span className="gap-badge">{gapLabel(pane.gap)}</span>}
        </div>
      })}
    </div>}
  </aside>
}
