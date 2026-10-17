import { memo, useEffect, useMemo, useRef, useState } from 'react'
import { hotkeysCoreFeature, selectionFeature, syncDataLoaderFeature } from '@headless-tree/core'
import { useTree } from '@headless-tree/react'
import { AgentStatusDot, gapLabel } from '../../shared/presentation'
import { agentNodeID, buildLaneNodes, buildSidebarNodes, buildSupervisionNodes, expandedLabel, laneHeaderID, treeSummary } from './sidebarNodes'
import type { SidebarNode } from './sidebarNodes'
import { agentKinds, reconcileExpansion } from './sidebarView'
import type { FleetView } from '../layout/shellPreferences'
import type { Board, Pane } from '../../types'
import { unattributedTerminalWarning } from '../screen/screenPresentation'
import { openInSideLabel, placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { TreeRow, TreeState } from '../../shared/TreeRow'
import { ContextUsed, contextUsedTooltip } from './ContextUsed'
import { LaunchAgent } from '../launch/LaunchAgent'
import { assignAgent, createLane, mutationProblem, renameAgent, updateLane, type LifecycleProblem } from '../../api/client'
import { beginRename, editingAt, prepareRename, renameValue, treeClickGuardSelector, type RenameState } from './renameModel'
import { laneHeaderTooltip, validateLaneName } from './laneDropModel'
import { dropAssignment, planSidebarDrop, type SidebarDrop } from './reparentModel'
import { useAgentRowMenu } from '../workspace/DockTabMenu.tsx'
import { DraftMark } from '../drafts/DraftMark.tsx'
import { useDraftMarks } from '../drafts/useDraftMarks.ts'

const emptyExpandedItems: string[] = []

// Memoised: Shell renders for dock and quick-open state the tree does not show.
export const FleetSidebar = memo(function FleetSidebar({ board, view, activeAgent, activeLane, onPreviewAgent, onPinAgent, onOpenTerminal, onOpenLaneSpace, expandedItems, onExpandedItems, knownWorkspaceItems, onKnownWorkspaceItems, knownManagerItems, onKnownManagerItems }: {
  board: Board | undefined
  view: FleetView
  activeAgent?: string
  // activeLane is the lane id of the active space when it is a lane space:
  // the lanes view expands that lane's header.
  activeLane?: string | null
  onPreviewAgent: (name: string, placement?: OpenPlacement) => void
  onPinAgent: (name: string, placement?: OpenPlacement) => void
  // A terminal herdr cannot attribute to an agent opens in the herdr view.
  onOpenTerminal: (pane: Pane) => void
  // Lanes view only: go to a lane's space.
  onOpenLaneSpace: (laneID: string) => void
  expandedItems: string[] | null
  onExpandedItems: (items: string[]) => void
  knownWorkspaceItems: string[] | null
  onKnownWorkspaceItems: (items: string[]) => void
  knownManagerItems: string[] | null
  onKnownManagerItems: (items: string[]) => void
}) {
  const agentRowMenu = useAgentRowMenu()
  const drafts = useDraftMarks()
  const [selectedItems, setSelectedItems] = useState<string[]>([])
  const [renaming, setRenaming] = useState<RenameState | null>(null)
  const [assignmentProblem, setAssignmentProblem] = useState<LifecycleProblem | null>(null)
  const [dragSource, setDragSource] = useState<string | null>(null)
  const [dropTarget, setDropTarget] = useState<string | null>(null)
  // newLane is the inline "+ lane" input: null when closed, else its text
  // and the last refusal shown under it. pendingLane is the name it took:
  // a header waiting for the first drop, which creates the lane with that
  // agent as lead. Neither is stored.
  const [newLane, setNewLane] = useState<{ value: string, problem: string | null } | null>(null)
  const [pendingLane, setPendingLane] = useState<string | null>(null)
  const renameInput = useRef<HTMLInputElement | null>(null)
  const cancelOnBlur = useRef(false)
  const placementNodes = useMemo(() => buildSidebarNodes(board), [board])
  const supervisionNodes = useMemo(() => buildSupervisionNodes(board), [board])
  const laneNodes = useMemo(() => buildLaneNodes(board, pendingLane), [board, pendingLane])
  const nodes = view === 'placement' ? placementNodes : view === 'lanes' ? laneNodes : supervisionNodes
  const sideHint = openInSideLabel(navigator.userAgent)

  const submitDrop = async (drop: SidebarDrop) => {
    setAssignmentProblem(null)
    try {
      if ('newLane' in drop) {
        await createLane(drop.newLane, drop.name)
        setPendingLane(null)
      } else await assignAgent(drop.name, drop.assignment)
    } catch (error) {
      setAssignmentProblem(mutationProblem(error))
    }
  }

  const makeLead = async (laneID: string, name: string) => {
    setAssignmentProblem(null)
    try {
      await updateLane(laneID, { lead: name })
    } catch (error) {
      setAssignmentProblem(mutationProblem(error))
    }
  }

  const startRename = (name: string, nodeID: string, title?: string) => {
    setAssignmentProblem(null)
    cancelOnBlur.current = false
    setRenaming(beginRename(name, nodeID, title))
  }

  // Select when the edited agent changes.
  useEffect(() => { if (renaming) renameInput.current?.select() }, [renaming?.nodeID])

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
    const next = reconcileExpansion(placementNodes, supervisionNodes, { expandedItems, knownWorkspaceItems, knownManagerItems }, laneNodes)
    if (!next) return
    if (next.expandedItems) onExpandedItems(next.expandedItems)
    if (next.knownWorkspaceItems) onKnownWorkspaceItems(next.knownWorkspaceItems)
    if (next.knownManagerItems) onKnownManagerItems(next.knownManagerItems)
  }, [board, expandedItems, laneNodes, knownManagerItems, knownWorkspaceItems, onExpandedItems, onKnownManagerItems, onKnownWorkspaceItems, placementNodes, supervisionNodes])

  // Entering a lane space opens that lane's header in the lanes view.
  const activeLaneHeader = activeLane ? laneHeaderID(activeLane) : null
  useEffect(() => {
    if (!activeLaneHeader || !expandedItems || expandedItems.includes(activeLaneHeader)) return
    onExpandedItems([...expandedItems, activeLaneHeader])
    // Only on entering the space: a header the operator folds afterwards stays folded.
  }, [activeLaneHeader])

  const commitNewLane = () => {
    if (!newLane) return
    const check = validateLaneName(newLane.value, (board?.lanes ?? []).map((lane) => lane.name))
    if (!check.ok) { setNewLane({ ...newLane, problem: check.reason }); return }
    setPendingLane(check.name)
    setNewLane(null)
  }

  useEffect(() => {
    if (!activeAgent) {
      setSelectedItems([])
      return
    }
    const matches = (node: SidebarNode | undefined) => !!node && agentKinds.has(node.kind) && node.pane?.agent === activeAgent
    const first = view === 'supervision' ? nodes.get(agentNodeID(activeAgent)) : [...nodes.values()].find(matches)
    // Every view draws an agent once, so its one row is the selection.
    setSelectedItems(first ? [first.id] : [])
  }, [activeAgent, nodes, view])

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
      else if (node.kind === 'pane' && node.pane?.agent === '-') onOpenTerminal(node.pane as Pane)
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
    {agentRowMenu.menu}
    {sidebarProblem && <div className="sidebar-problem" role="alert">{sidebarProblem.readOnly ?? sidebarProblem.banner ?? sidebarProblem.inline}</div>}
    {!board ? <TreeState depth={0} title="Waiting for fleet…" /> : <div {...tree.getContainerProps(view === 'placement' ? 'Workspaces and agents' : view === 'lanes' ? 'Lanes' : 'Supervision tree')}
      className={`fleet-tree panel-tree fleet-tree-${view}${dropTarget === 'tree-root' ? ' drop-target' : ''}`}
      onDragOver={(event) => {
        if (event.target !== event.currentTarget) return
        const result = planSidebarDrop(view, dragSource, null, nodes)
        if (!result) return
        event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setDropTarget('tree-root')
      }}
      onDragLeave={(event) => { if (event.target === event.currentTarget) setDropTarget(null) }}
      onDrop={(event) => {
        if (event.target !== event.currentTarget) return
        setDropTarget(null)
        if (dropAssignment(view, dragSource, null, nodes, submitDrop)) event.preventDefault()
      }}>
      {view === 'lanes' && <div className="group-create-row" role="none">
        {newLane
          ? <><input className="rename-agent-input group-create-input" aria-label="New lane name" placeholder="lane name" value={newLane.value} autoFocus
            onChange={(event) => setNewLane({ value: event.target.value, problem: null })}
            onBlur={() => setNewLane(null)}
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => {
              event.stopPropagation()
              if (event.key === 'Enter') commitNewLane()
              if (event.key === 'Escape') setNewLane(null)
            }} />
            {newLane.problem && <span className="sidebar-problem group-create-problem" role="alert">{newLane.problem}</span>}</>
          : <button type="button" className="group-space-button group-create-button" aria-label="New lane" title="New lane: the first agent dropped on it becomes its lead"
            onClick={(event) => { event.stopPropagation(); setNewLane({ value: '', problem: null }) }}>+ lane</button>}
      </div>}
      {tree.getItems().map((item) => {
        const node = item.getItemData()
        const pane = node.pane
        const editing = pane ? editingAt(renaming, node.id) : null
        const signal = node.statusText ?? ''
        const folder = item.isFolder()
        const treeItemProps = item.getProps()
        const agentRow = agentKinds.has(node.kind)
        const groupHeader = node.kind === 'lane' || node.kind === 'no-lane'
        const draggableAgent = view !== 'placement' && (node.kind === 'agent' || node.kind === 'subagent') && !!pane?.agent && pane.agent !== '-' && pane.bus_status !== '-' && !editing
        const memberCount = groupHeader ? node.summary?.total ?? 0 : 0
        const leadChip = node.lead ? <span className="group-chip" title="lane lead">lead</span> : null
        const folded = folder && !item.isExpanded() && node.summary !== undefined
        const summary = treeSummary(node, folded)
        const icon = pane?.agent && pane.agent !== '-' ? <AgentStatusDot status={pane.bus_status} />
          : pane?.agent === '-' ? <span className="terminal-glyph">›_</span>
            : node.kind === 'tombstone' ? <span className="tombstone-glyph">⊘</span>
              : <span>▰</span>
        return <TreeRow
          key={item.getId()}
          itemProps={{
            ...treeItemProps,
            // One drag seam per row: the plan (group header vs manager row) is
            // chosen by view inside planSidebarDrop; the placement view never drags.
            draggable: draggableAgent,
            onDragStart: (event) => {
              event.dataTransfer.setData('text/plain', item.getId())
              event.dataTransfer.effectAllowed = 'move'
              setDragSource(item.getId())
            },
            onDragEnd: () => { setDragSource(null); setDropTarget(null) },
            onDragOver: (event) => {
              const result = planSidebarDrop(view, dragSource, item.getId(), nodes)
              if (!result) return
              event.preventDefault(); event.stopPropagation(); event.dataTransfer.dropEffect = 'move'; setDropTarget(item.getId())
            },
            onDragLeave: () => { if (dropTarget === item.getId()) setDropTarget(null) },
            onDrop: (event) => {
              event.stopPropagation()
              setDropTarget(null)
              if (dropAssignment(view, dragSource, item.getId(), nodes, submitDrop)) event.preventDefault()
            },
            onFocus: () => item.setFocused(),
            onContextMenu: pane?.agent && pane.agent !== '-' && pane.bus_status !== '-'
              ? (event) => agentRowMenu.open(event, pane.agent, view === 'lanes' && node.lane && !node.lead
                ? { label: 'Make lead', run: () => { void makeLead(node.lane!, pane.agent) } }
                : null)
              : undefined,
            onClick: () => { item.setFocused(); setSelectedItems([item.getId()]); item.primaryAction() },
            onClickCapture: (event) => {
              if (!event.altKey || (event.target as Element).closest(treeClickGuardSelector)) return
              event.preventDefault()
              event.stopPropagation()
              const placement = placementFromModifiers(event)
              if (pane?.agent && pane.agent !== '-') onPreviewAgent(pane.agent, placement)
              else if (node.kind === 'pane' && pane?.agent === '-') onOpenTerminal(pane as Pane)
            },
            onDoubleClick: (event) => {
              if (editing || (event.target as Element).closest(treeClickGuardSelector)) return
              const placement = placementFromModifiers(event)
              if (pane?.agent && pane.agent !== '-') onPinAgent(pane.agent, placement)
            },
            onKeyDown: (event) => {
              if (event.key === 'F2' && pane?.agent && pane.agent !== '-') {
                event.preventDefault()
                event.stopPropagation()
                startRename(pane.agent, node.id, pane.title)
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
          className={`${agentRow ? 'pane-row' : 'workspace-row'}${pane?.agent && pane.agent !== '-' ? ' agent-row' : ''}${pane?.agent === '-' ? ' shell-row' : ''}${node.kind === 'unplaced' ? ' unplaced-row' : ''}${node.kind === 'subagent' ? ' subagent-row' : ''}${node.kind === 'tombstone' ? ' tombstone-row' : ''}${groupHeader ? ' group-row' : ''}${dropTarget === node.id ? ' drop-target' : ''}${dragSource === node.id ? ' dragging' : ''}`}
          icon={icon}
          label={editing
            ? <span className="tree-label"><input ref={renameInput} className="rename-agent-input" aria-label={`Rename ${editing.name}`} value={editing.value} maxLength={80}
              onChange={(event) => setRenaming(renameValue(editing, event.target.value))} onBlur={() => { void finishRename() }}
              onClick={(event) => event.stopPropagation()} onDoubleClick={(event) => event.stopPropagation()} onKeyDown={(event) => {
                event.stopPropagation()
                if (event.key === 'Enter') event.currentTarget.blur()
                if (event.key === 'Escape') { cancelOnBlur.current = true; event.currentTarget.blur() }
              }} /></span>
            : <><span className="tree-label" title={summary ? `${expandedLabel(node)} ${summary}` : expandedLabel(node)}>{node.name}{node.secondary && <span className="tree-secondary">{` · ${node.secondary}`}</span>}{summary && <span className="tree-summary"> {summary}</span>}</span>{leadChip}</>}
          trailing={<>{node.marker === 'unknown-manager' && <span className="unknown-manager-marker" title="manager unknown · adopt to take it on">?</span>}
            {node.kind === 'workspace' && node.workspace && <LaunchAgent workspaceID={node.workspace.workspace_id} workspaceName={node.name} checkoutPath={node.workspace.cwd} onOpenAgent={onPreviewAgent} />}
            {node.kind === 'lane' && node.lane && <button type="button" className="group-space-button" aria-label={`Open space ${node.name}`} title={`Open space ${node.name}`}
              onClick={(event) => { event.stopPropagation(); onOpenLaneSpace(node.lane!) }}>space</button>}
            {node.placeholder && <button type="button" className="group-space-button group-remove-button" aria-label={`Remove new lane ${node.name}`} title={`Remove new lane ${node.name} (nothing was written)`}
              onClick={(event) => { event.stopPropagation(); setPendingLane(null) }}>×</button>}
            {pane?.agent && pane.agent !== '-' && <ContextUsed value={node.contextUsed} />}
            {pane?.agent && pane.agent !== '-' && !renaming && <button type="button" className="rename-agent-button" aria-label={`Rename ${pane.agent}`} title={`Rename ${pane.agent}`}
              onClick={(event) => { event.stopPropagation(); startRename(pane.agent, node.id, pane.title) }}>✎</button>}
            {view === 'supervision' && node.marker === 'unknown-manager' && pane?.agent && pane.agent !== '-' && <button type="button" className="rename-agent-button adopt-agent-button" aria-label={`Adopt ${pane.agent}`} title={`Adopt ${pane.agent}: set its manager to you (human)`}
              onClick={(event) => { event.stopPropagation(); void submitDrop({ name: pane.agent, assignment: { manager: 'human' } }) }}>adopt</button>}
            {folder && !folded && !groupHeader && <span className="count-badge">{node.count ?? node.summary?.total ?? node.children.length}</span>}
            {pane?.agent && pane.agent !== '-' && <DraftMark drafts={drafts[pane.agent]} />}
            {signal && <span className="bus-status">{signal}</span>}
            {pane && pane.agent !== '-' && pane.gap !== '-' && <span className="gap-badge">{gapLabel(pane.gap)}</span>}</>}
          title={pane ? pane.agent === '-' ? `${pane.pane_id} · ${unattributedTerminalWarning} · ${sideHint}` : `${pane.title ? `${pane.agent} · ` : ''}${node.workspaceLabel ? `${node.workspaceLabel} · ` : ''}${pane.parent_agent ? `subagent of ${pane.parent_agent}` : pane.pane_id}${node.tabLabel ? ` · ${node.tabLabel}` : ''}${pane.manager ? ` · manager ${pane.manager}${pane.manager_state && pane.manager_state !== 'live' ? ` (${pane.manager_state})` : ''}` : ''} · ${pane.tool} · herdr ${pane.herdr_status}${signal ? ` · bus ${signal}` : ''}${contextUsedTooltip(node.contextUsed)}${node.lead ? ' · lane lead' : ''} · ${sideHint}` : groupHeader ? laneHeaderTooltip(node, memberCount) : node.kind === 'tombstone' ? `${node.name} · ended · its reports wait here until reparented` : node.name}
          onToggle={() => { if (item.isExpanded()) item.collapse(); else item.expand() }}
        />
      })}
    </div>}
  </div>
})
