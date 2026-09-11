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
import { ContextUsed, contextUsedTooltip } from './ContextUsed'
import { LaunchAgent } from '../launch/LaunchAgent'
import { apiProblem, assignAgent, lifecycleProblem, renameAgent, viewerReadOnlyMessage, type AssignmentPatch, type LifecycleProblem } from '../../api/client'
import { beginRename, prepareRename, renameValue, treeClickGuardSelector, type RenameState } from './renameModel'
import { dropAssignment, reparentDrop } from './reparentModel'

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
  const [assignmentProblem, setAssignmentProblem] = useState<LifecycleProblem | null>(null)
  const [dragSource, setDragSource] = useState<string | null>(null)
  const [dropTarget, setDropTarget] = useState<string | null>(null)
  const renameInput = useRef<HTMLInputElement | null>(null)
  const cancelOnBlur = useRef(false)
  const placementNodes = useMemo(() => buildSidebarNodes(board), [board])
  const supervisionNodes = useMemo(() => buildSupervisionNodes(board), [board])
  const nodes = view === 'placement' ? placementNodes : supervisionNodes
  const sideHint = openInSideLabel(navigator.userAgent)

  const mutationProblem = (error: unknown) => {
    const { response, problem } = apiProblem(error)
    return response?.status === 409 && (problem.error === 'attribution required' || problem.error === 'sender refused')
      ? { readOnly: viewerReadOnlyMessage(problem, response.status) }
      : lifecycleProblem(error)
  }

  const submitAssignment = async (name: string, assignment: AssignmentPatch) => {
    setAssignmentProblem(null)
    try {
      await assignAgent(name, assignment)
    } catch (error) {
      setAssignmentProblem(mutationProblem(error))
    }
  }

  const startRename = (name: string, title?: string) => {
    setAssignmentProblem(null)
    cancelOnBlur.current = false
    setRenaming(beginRename(name, title))
  }

  // Select when the edited agent changes.
  useEffect(() => { if (renaming) renameInput.current?.select() }, [renaming?.name])

  const finishRename = async () => {
    if (!renaming) return
    if (cancelOnBlur.current) {
      cancelOnBlur.current = false
      setRenaming(null)
      return
    }
    const title = prepareRename(renaming)
    if (!title) {
      setRenaming(null)
      return
    }
    const submittedName = renaming.name
    try {
      await renameAgent(renaming.name, title)
      setRenaming((current) => current?.name === submittedName ? null : current)
    } catch (error) {
      const refusal = mutationProblem(error)
      setRenaming((current) => current?.name === submittedName ? { ...current, problem: refusal } : current)
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

  const sidebarProblem = assignmentProblem ?? renaming?.problem
  return <div className="fleet-sidebar-view">
    {sidebarProblem && <div className="sidebar-problem" role="alert">{sidebarProblem.readOnly ?? sidebarProblem.banner ?? sidebarProblem.inline}</div>}
    {!board ? <TreeState depth={0} title="Waiting for fleet…" /> : <div {...tree.getContainerProps(view === 'placement' ? 'Workspaces and agents' : 'Supervision tree')}
      className={`fleet-tree panel-tree fleet-tree-${view}${dropTarget === 'tree-root' ? ' drop-target' : ''}`}
      onDragOver={(event) => {
        if (view !== 'supervision' || event.target !== event.currentTarget) return
        const result = reparentDrop(view, dragSource, null, nodes)
        if ('refusal' in result) return
        event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setDropTarget('tree-root')
      }}
      onDragLeave={(event) => { if (event.target === event.currentTarget) setDropTarget(null) }}
      onDrop={(event) => {
        if (event.target !== event.currentTarget) return
        setDropTarget(null)
        if (dropAssignment(view, dragSource, null, nodes, submitAssignment)) event.preventDefault()
      }}>
      {tree.getItems().map((item) => {
        const node = item.getItemData()
        const pane = node.pane
        const editing = pane && renaming?.name === pane.agent ? renaming : null
        const signal = node.statusText ?? ''
        const folder = item.isFolder()
        const treeItemProps = item.getProps()
        const agentRow = agentKinds.has(node.kind)
        const folded = folder && !item.isExpanded() && node.summary !== undefined
        const icon = pane?.agent && pane.agent !== '-' ? <AgentStatusDot status={pane.bus_status} />
          : pane?.agent === '-' ? <span className="terminal-glyph">›_</span>
            : node.kind === 'tombstone' ? <span className="tombstone-glyph">⊘</span>
              : <span>▰</span>
        return <TreeRow
          key={item.getId()}
          itemProps={{
            ...treeItemProps,
            draggable: view === 'supervision' && (node.kind === 'agent' || node.kind === 'subagent') && pane?.bus_status !== '-',
            onDragStart: (event) => {
              event.dataTransfer.setData('text/plain', item.getId())
              event.dataTransfer.effectAllowed = 'move'
              setDragSource(item.getId())
            },
            onDragEnd: () => { setDragSource(null); setDropTarget(null) },
            onDragOver: (event) => {
              const result = reparentDrop(view, dragSource, item.getId(), nodes)
              if ('refusal' in result) return
              event.preventDefault(); event.stopPropagation(); event.dataTransfer.dropEffect = 'move'; setDropTarget(item.getId())
            },
            onDragLeave: () => { if (dropTarget === item.getId()) setDropTarget(null) },
            onDrop: (event) => {
              event.stopPropagation()
              setDropTarget(null)
              if (dropAssignment(view, dragSource, item.getId(), nodes, submitAssignment)) event.preventDefault()
            },
            onFocus: () => item.setFocused(),
            onClick: () => { item.setFocused(); setSelectedItems([item.getId()]); item.primaryAction() },
            onClickCapture: (event) => {
              if (!event.altKey || (event.target as Element).closest(treeClickGuardSelector)) return
              event.preventDefault()
              event.stopPropagation()
              const placement = placementFromModifiers(event)
              if (pane?.agent && pane.agent !== '-') onPreviewAgent(pane.agent, placement)
              else if (node.kind === 'pane' && pane?.agent === '-') onPreviewPane(pane as Pane, placement)
            },
            onDoubleClick: (event) => {
              if (editing || (event.target as Element).closest(treeClickGuardSelector)) return
              const placement = placementFromModifiers(event)
              if (pane?.agent && pane.agent !== '-') onPinAgent(pane.agent, placement)
              else if (node.kind === 'pane' && pane?.agent === '-') onPinPane(pane as Pane, placement)
            },
            onKeyDown: (event) => {
              if (event.key === 'F2' && pane?.agent && pane.agent !== '-') {
                event.preventDefault()
                event.stopPropagation()
                startRename(pane.agent, pane.title)
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
          className={`${agentRow ? 'pane-row' : 'workspace-row'}${pane?.agent && pane.agent !== '-' ? ' agent-row' : ''}${pane?.agent === '-' ? ' shell-row' : ''}${node.kind === 'unplaced' ? ' unplaced-row' : ''}${node.kind === 'subagent' ? ' subagent-row' : ''}${node.kind === 'tombstone' ? ' tombstone-row' : ''}${dropTarget === node.id ? ' drop-target' : ''}${dragSource === node.id ? ' dragging' : ''}`}
          icon={icon}
          label={editing
            ? <span className="tree-label"><input ref={renameInput} className="rename-agent-input" aria-label={`Rename ${editing.name}`} value={editing.value} maxLength={80}
              onChange={(event) => setRenaming(renameValue(editing, event.target.value))} onBlur={() => { void finishRename() }}
              onClick={(event) => event.stopPropagation()} onDoubleClick={(event) => event.stopPropagation()} onKeyDown={(event) => {
                event.stopPropagation()
                if (event.key === 'Enter') event.currentTarget.blur()
                if (event.key === 'Escape') { cancelOnBlur.current = true; event.currentTarget.blur() }
              }} /></span>
            : <span className="tree-label" title={folded ? collapsedLabel(node) : expandedLabel(node)}>{node.name}{node.secondary && <span className="tree-secondary">{` · ${node.secondary}`}</span>}{folded && node.summary && node.summary.total > 0 && <span className="tree-summary"> ({node.summary.total}{agentRow ? '' : ` · ${node.summary.active} active`})</span>}</span>}
          trailing={<>{node.marker === 'unknown-manager' && <span className="unknown-manager-marker" title="manager unknown · adopt to take it on">?</span>}
            {node.kind === 'workspace' && node.workspace && <LaunchAgent workspaceID={node.workspace.workspace_id} workspaceName={node.name} checkoutPath={node.workspace.cwd} onOpenAgent={onPreviewAgent} />}
            {pane?.agent && pane.agent !== '-' && <ContextUsed value={node.contextUsed} />}
            {pane?.agent && pane.agent !== '-' && !renaming && <button type="button" className="rename-agent-button" aria-label={`Rename ${pane.agent}`} title={`Rename ${pane.agent}`}
              onClick={(event) => { event.stopPropagation(); startRename(pane.agent, pane.title) }}>✎</button>}
            {view === 'supervision' && node.marker === 'unknown-manager' && pane?.agent && pane.agent !== '-' && <button type="button" className="rename-agent-button adopt-agent-button" aria-label={`Adopt ${pane.agent}`} title={`Adopt ${pane.agent}: set its manager to you (human)`}
              onClick={(event) => { event.stopPropagation(); void submitAssignment(pane.agent, { manager: 'human' }) }}>adopt</button>}
            {folder && !folded && <span className="count-badge">{node.count ?? node.summary?.total ?? node.children.length}</span>}
            {signal && <span className="bus-status">{signal}</span>}
            {pane && pane.agent !== '-' && pane.gap !== '-' && <span className="gap-badge">{gapLabel(pane.gap)}</span>}</>}
          title={pane ? pane.agent === '-' ? `${pane.pane_id} · ${unattributedTerminalWarning} · ${sideHint}` : `${pane.title ? `${pane.agent} · ` : ''}${node.workspaceLabel ? `${node.workspaceLabel} · ` : ''}${pane.parent_agent ? `subagent of ${pane.parent_agent}` : pane.pane_id}${node.tabLabel ? ` · ${node.tabLabel}` : ''}${pane.manager ? ` · manager ${pane.manager}${pane.manager_state && pane.manager_state !== 'live' ? ` (${pane.manager_state})` : ''}` : ''} · ${pane.tool} · herdr ${pane.herdr_status}${signal ? ` · bus ${signal}` : ''}${contextUsedTooltip(node.contextUsed)} · ${sideHint}` : node.kind === 'tombstone' ? `${node.name} · ended · its reports wait here until reparented` : node.name}
          onToggle={() => { if (item.isExpanded()) item.collapse(); else item.expand() }}
        />
      })}
    </div>}
  </div>
}
