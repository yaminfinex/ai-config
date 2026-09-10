import { useEffect, useMemo, useRef, useState } from 'react'
import { hotkeysCoreFeature, selectionFeature, syncDataLoaderFeature } from '@headless-tree/core'
import { useTree } from '@headless-tree/react'
import { AgentStatusDot, gapLabel } from '../../shared/presentation'
import { agentNodeID, buildSidebarNodes, buildSupervisionNodes, collapsedLabel, expandedLabel } from './sidebarNodes'
import type { SidebarNode } from './sidebarNodes'
import { agentKinds, reconcileExpansion } from './sidebarView'
import type { FleetView } from '../layout/shellPreferences'
import type { Board, Pane } from '../../types'
import { unattributedTerminalWarning } from '../screen/screenPresentation'
import { openInSideLabel, placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { TreeRow, TreeState } from '../../shared/TreeRow'
import { LaunchAgent } from '../launch/LaunchAgent'
import { apiProblem, lifecycleProblem, renameAgent, viewerReadOnlyMessage } from '../../api/client'
import { beginRename, cancelRename, prepareRename, renameRefused, renameValue, treeClickGuardSelector, type RenameState } from './renameModel'

const emptyExpandedItems: string[] = []

export function FleetSidebar({ board, view, activeAgent, activePane, onPreviewAgent, onPinAgent, onPreviewPane, onPinPane, expandedItems, onExpandedItems, knownWorkspaceItems, onKnownWorkspaceItems, knownManagerItems, onKnownManagerItems }: {
  board: Board | undefined
  view: FleetView
  activeAgent?: string
  activePane?: string
  onPreviewAgent: (name: string, placement?: OpenPlacement) => void
  onPinAgent: (name: string, placement?: OpenPlacement) => void
  onPreviewPane: (pane: Pane, placement?: OpenPlacement) => void
  onPinPane: (pane: Pane, placement?: OpenPlacement) => void
  expandedItems: string[] | null
  onExpandedItems: (items: string[]) => void
  knownWorkspaceItems: string[] | null
  onKnownWorkspaceItems: (items: string[]) => void
  knownManagerItems: string[] | null
  onKnownManagerItems: (items: string[]) => void
}) {
  const [selectedItems, setSelectedItems] = useState<string[]>([])
  const [renaming, setRenaming] = useState<RenameState | null>(null)
  const renameInput = useRef<HTMLInputElement | null>(null)
  const cancelOnBlur = useRef(false)
  const renamePending = useRef(false)
  const placementNodes = useMemo(() => buildSidebarNodes(board), [board])
  const supervisionNodes = useMemo(() => buildSupervisionNodes(board), [board])
  const nodes = view === 'placement' ? placementNodes : supervisionNodes
  const sideHint = openInSideLabel(navigator.userAgent)

  useEffect(() => { if (renaming) renameInput.current?.select() }, [renaming?.name])

  const finishRename = async () => {
    if (!renaming) return
    if (cancelOnBlur.current) {
      cancelOnBlur.current = false
      setRenaming(cancelRename())
      return
    }
    const request = prepareRename(renaming)
    if (!request.title) {
      setRenaming(null)
      return
    }
    if (renamePending.current) return
    renamePending.current = true
    const submittedName = renaming.name
    try {
      await renameAgent(renaming.name, request.title)
      setRenaming((current) => current?.name === submittedName ? cancelRename() : current)
    } catch (error) {
      const { response, problem } = apiProblem(error)
      const refusal = response?.status === 409 && (problem.error === 'attribution required' || problem.error === 'sender refused')
        ? { readOnly: viewerReadOnlyMessage(problem, response.status) }
        : lifecycleProblem(error)
      setRenaming((current) => current?.name === submittedName ? renameRefused(current, refusal) : current)
    } finally {
      renamePending.current = false
    }
  }

  useEffect(() => {
    if (!board) return
    const next = reconcileExpansion(placementNodes, supervisionNodes, { expandedItems, knownWorkspaceItems, knownManagerItems })
    if (!next) return
    if (next.expandedItems) onExpandedItems(next.expandedItems)
    if (next.knownWorkspaceItems) onKnownWorkspaceItems(next.knownWorkspaceItems)
    if (next.knownManagerItems) onKnownManagerItems(next.knownManagerItems)
  }, [board, expandedItems, knownManagerItems, knownWorkspaceItems, onExpandedItems, onKnownManagerItems, onKnownWorkspaceItems, placementNodes, supervisionNodes])

  useEffect(() => {
    if (!activeAgent && !activePane) {
      setSelectedItems([])
      return
    }
    const match = activeAgent && view === 'supervision' ? nodes.get(agentNodeID(activeAgent))
      : [...nodes.values()].find((node) => agentKinds.has(node.kind) && (activeAgent ? node.pane?.agent === activeAgent : node.pane?.pane_id === activePane))
    setSelectedItems(match ? [match.id] : [])
  }, [activeAgent, activePane, nodes, view])

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

  const renameProblem = renaming?.problem
  return <div className="fleet-sidebar-view">
    {renameProblem && <div className="rename-agent-problem" role="alert">{renameProblem.readOnly ?? renameProblem.banner ?? renameProblem.inline}</div>}
    {!board ? <TreeState depth={0} title="Waiting for fleet…" /> : <div {...tree.getContainerProps(view === 'placement' ? 'Workspaces and agents' : 'Supervision tree')} className={`fleet-tree panel-tree fleet-tree-${view}`}>
      {tree.getItems().map((item) => {
        const node = item.getItemData()
        const pane = node.pane
        const edit = pane && renaming?.name === pane.agent ? renaming : null
        const signal = node.statusText ?? ''
        const folder = item.isFolder()
        const treeItemProps = item.getProps()
        const agentRow = agentKinds.has(node.kind)
        const folded = folder && !item.isExpanded() && node.summary !== undefined
        const icon = pane?.agent && pane.agent !== '-' ? <AgentStatusDot status={pane.bus_status} />
          : pane?.agent === '-' ? <span className="terminal-glyph">›_</span>
            : node.kind === 'operator' ? <span>◉</span>
              : node.kind === 'tombstone' ? <span className="tombstone-glyph">⊘</span>
                : <span>▰</span>
        return <TreeRow
          key={item.getId()}
          itemProps={{
            ...treeItemProps,
            onFocus: () => item.setFocused(),
            onClickCapture: (event) => {
              if (!event.altKey || (event.target as Element).closest(treeClickGuardSelector)) return
              event.preventDefault()
              event.stopPropagation()
              const placement = placementFromModifiers(event)
              if (pane?.agent && pane.agent !== '-') onPreviewAgent(pane.agent, placement)
              else if (node.kind === 'pane' && pane?.agent === '-') onPreviewPane(pane as Pane, placement)
            },
            onDoubleClick: (event) => {
              if (edit || (event.target as Element).closest(treeClickGuardSelector)) return
              const placement = placementFromModifiers(event)
              if (pane?.agent && pane.agent !== '-') onPinAgent(pane.agent, placement)
              else if (node.kind === 'pane' && pane?.agent === '-') onPinPane(pane as Pane, placement)
            },
            onKeyDown: (event) => {
              if (event.key === 'F2' && pane?.agent && pane.agent !== '-') {
                event.preventDefault()
                event.stopPropagation()
                cancelOnBlur.current = false
                setRenaming(beginRename(pane.agent, pane.title))
                return
              }
              treeItemProps.onKeyDown?.(event)
            },
          }}
          depth={item.getItemMeta().level}
          name={node.name}
          expandable={folder}
          expanded={item.isExpanded()}
          selected={item.isSelected()}
          focused={item.isFocused()}
          className={`${agentRow ? 'pane-row' : 'workspace-row'}${pane?.agent && pane.agent !== '-' ? ' agent-row' : ''}${pane?.agent === '-' ? ' shell-row' : ''}${node.kind === 'unplaced' || node.kind === 'unadopted' ? ' unplaced-row' : ''}${node.kind === 'subagent' ? ' subagent-row' : ''}${node.kind === 'tombstone' ? ' tombstone-row' : ''}${node.kind === 'unknown-manager' ? ' unknown-manager-row' : ''}${node.kind === 'operator' ? ' operator-row' : ''}`}
          icon={icon}
          label={edit
            ? <span className="tree-label"><input ref={renameInput} className="rename-agent-input" aria-label={`Rename ${edit.name}`} value={edit.value} maxLength={80}
              onChange={(event) => setRenaming(renameValue(edit, event.target.value))} onBlur={() => { void finishRename() }}
              onDoubleClick={(event) => event.stopPropagation()} onKeyDown={(event) => {
                event.stopPropagation()
                if (event.key === 'Enter') event.currentTarget.blur()
                if (event.key === 'Escape') { cancelOnBlur.current = true; event.currentTarget.blur() }
              }} /></span>
            : <span className="tree-label" title={folded ? collapsedLabel(node) : expandedLabel(node)}>{node.name}{node.secondary && <span className="tree-secondary">{` · ${node.secondary}`}</span>}{folded && node.summary && node.summary.total > 0 && <span className="tree-summary"> ({node.summary.total} · {node.summary.active} active)</span>}</span>}
          trailing={<>{node.kind === 'workspace' && node.workspace && <LaunchAgent workspaceID={node.workspace.workspace_id} workspaceName={node.name} checkoutPath={node.workspace.cwd} onOpenAgent={onPreviewAgent} />}
            {pane?.agent && pane.agent !== '-' && !renaming && <button type="button" className="rename-agent-button" aria-label={`Rename ${pane.agent}`} title={`Rename ${pane.agent}`}
              onClick={(event) => { event.stopPropagation(); cancelOnBlur.current = false; setRenaming(beginRename(pane.agent, pane.title)) }}>✎</button>}
            {folder && !folded && <span className="count-badge">{node.count ?? node.summary?.total ?? node.children.length}</span>}
            {signal && <span className="bus-status">{signal}</span>}
            {pane && pane.agent !== '-' && pane.gap !== '-' && <span className="gap-badge">{gapLabel(pane.gap)}</span>}
            {node.paneChip && <span className="pane-chip" title={node.workspaceLabel}>{node.paneChip}</span>}</>}
          title={pane ? pane.agent === '-' ? `${pane.pane_id} · ${unattributedTerminalWarning} · ${sideHint}` : `${node.workspaceLabel ? `${node.workspaceLabel} · ` : ''}${pane.parent_agent ? `subagent of ${pane.parent_agent}` : pane.pane_id}${node.tabLabel ? ` · ${node.tabLabel}` : ''}${pane.manager ? ` · manager ${pane.manager}${pane.manager_state && pane.manager_state !== 'live' ? ` (${pane.manager_state})` : ''}` : ''} · ${pane.tool} · herdr ${pane.herdr_status}${signal ? ` · bus ${signal}` : ''} · ${sideHint}` : node.kind === 'tombstone' ? `${node.name} · ended · its reports wait here until reparented` : node.kind === 'unknown-manager' ? `${node.name} · no live seat or record by this name` : node.name}
          onToggle={() => { if (item.isExpanded()) item.collapse(); else item.expand() }}
        />
      })}
    </div>}
  </div>
}
