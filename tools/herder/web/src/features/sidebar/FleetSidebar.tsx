import { useEffect, useMemo, useState } from 'react'
import { hotkeysCoreFeature, selectionFeature, syncDataLoaderFeature } from '@headless-tree/core'
import { useTree } from '@headless-tree/react'
import { AgentStatusDot, gapLabel } from '../../shared/presentation'
import { buildSidebarNodes } from './sidebarNodes'
import type { SidebarNode } from './sidebarNodes'
import type { Board, Pane } from '../../types'
import { unattributedTerminalWarning } from '../screen/screenPresentation'

const emptyExpandedItems: string[] = []

export function FleetSidebar({ board, activeAgent, activePane, onPreviewAgent, onPinAgent, onPreviewPane, onPinPane, expandedItems, onExpandedItems, knownWorkspaceItems, onKnownWorkspaceItems }: {
  board: Board | undefined
  activeAgent?: string
  activePane?: string
  onPreviewAgent: (name: string) => void
  onPinAgent: (name: string) => void
  onPreviewPane: (pane: Pane) => void
  onPinPane: (pane: Pane) => void
  expandedItems: string[] | null
  onExpandedItems: (items: string[]) => void
  knownWorkspaceItems: string[] | null
  onKnownWorkspaceItems: (items: string[]) => void
}) {
  const [selectedItems, setSelectedItems] = useState<string[]>([])
  const nodes = useMemo(() => buildSidebarNodes(board), [board])

  useEffect(() => {
    if (!board) return
    const workspaceItems = [...nodes.values()].filter((node) => node.kind === 'workspace').map((node) => node.id)
    if (expandedItems === null) {
      onExpandedItems([...nodes.values()].filter((node) => node.kind === 'workspace' || node.kind === 'unplaced' || ((node.kind === 'pane' || node.kind === 'subagent') && node.children.length > 0)).map((node) => node.id))
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
    if (!activeAgent && !activePane) {
      setSelectedItems([])
      return
    }
    const match = [...nodes.values()].find((node) => (node.kind === 'pane' || node.kind === 'subagent') && (activeAgent ? node.pane?.agent === activeAgent : node.pane?.pane_id === activePane))
    setSelectedItems(match ? [match.id] : [])
  }, [activeAgent, activePane, nodes])

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
      if (node.pane?.agent && node.pane.agent !== '-') onPreviewAgent(node.pane.agent)
      else if (node.kind === 'pane' && node.pane?.agent === '-') onPreviewPane(node.pane as Pane)
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
          className={`tree-row ${node.kind === 'pane' || node.kind === 'subagent' ? 'pane-row' : 'workspace-row'}${pane?.agent && pane.agent !== '-' ? ' agent-row' : ''}${pane?.agent === '-' ? ' shell-row' : ''}${node.kind === 'unplaced' ? ' unplaced-row' : ''}${node.kind === 'subagent' ? ' subagent-row' : ''}${item.isFocused() ? ' tree-focused' : ''}${item.isSelected() ? ' selected' : ''}`}
          style={{ paddingLeft: `${item.getItemMeta().level * 16 + 5}px` }}
          title={pane ? pane.agent === '-' ? `${pane.pane_id} · ${unattributedTerminalWarning}` : `${pane.parent_agent ? `subagent of ${pane.parent_agent}` : pane.pane_id}${node.tabLabel ? ` · ${node.tabLabel}` : ''} · ${pane.tool} · herdr ${pane.herdr_status}${signal ? ` · bus ${signal}` : ''}` : node.name}
          onFocus={() => item.setFocused()}
          onDoubleClick={() => { if (pane?.agent && pane.agent !== '-') onPinAgent(pane.agent); else if (node.kind === 'pane' && pane?.agent === '-') onPinPane(pane as Pane) }}
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
