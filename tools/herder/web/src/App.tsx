import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiProblem, getFleet, queryKeys, viewerReadOnlyMessage } from './api/client'
import { viewerQueryOptions } from './api/queries'
import { BoardPanel } from './features/board/BoardPanel'
import { FleetSidebar } from './features/sidebar/FleetSidebar'
import { AgentPanel } from './features/transcript/AgentPanel'
import { ScreenPanel } from './features/screen/ScreenPanel'
import { AppLink, currentRoute, type Route } from './shared/navigation'
import { agentBusStatus } from './shared/agentStatus'
import { AgentStatusDot, Banner } from './shared/presentation'
import { ThemeToggle } from './shared/ThemeToggle'
import { useFleetStream } from './stream/useFleetStream'
import { agentTabID, applyRoute, autoPinPreview, createTabState, pinAgent, previewAgent, storedPinnedAgents, type AgentTabState } from './previewTabs'
import type { Pane } from './types'

const layoutKey = 'herder.web.layout.v1'
const boardTab = { id: 'board', kind: 'board' as const, label: 'Board' }
const defaultSidebarWidth = 250

type ShellTab = typeof boardTab | { id: string, kind: 'agent', label: string, name: string, preview: boolean } | { id: string, kind: 'screen', label: string, pane: Pane, preview: boolean }
type StoredLayout = {
  openTabs: string[]
  activeTab: string
  sidebarWidth: number
  expandedItems?: string[]
  knownWorkspaceItems?: string[]
}

function agentTab(name: string, preview = false): ShellTab {
  return { id: agentTabID(name), kind: 'agent', label: name, name, preview }
}

function screenTabID(paneID: string) { return `screen:${paneID}` }
function screenTab(pane: Pane, preview = false): ShellTab { return { id: screenTabID(pane.pane_id), kind: 'screen', label: pane.label || pane.pane_id, pane, preview } }

function clampSidebarWidth(width: number) {
  return Math.min(440, Math.max(200, width))
}

function readLayout(): {
  tabs: ShellTab[]
  activeTab: string
  sidebarWidth: number
  expandedItems: string[] | null
  knownWorkspaceItems: string[] | null
} {
  try {
    const stored = JSON.parse(localStorage.getItem(layoutKey) ?? '') as Partial<StoredLayout>
    if (!Array.isArray(stored.openTabs) || stored.openTabs.some((name) => typeof name !== 'string' || !name) ||
      typeof stored.activeTab !== 'string' || typeof stored.sidebarWidth !== 'number' || !Number.isFinite(stored.sidebarWidth)) throw new Error('invalid layout')
    const tabs = [boardTab, ...[...new Set(stored.openTabs)].map((name) => agentTab(name))]
    if (!tabs.some((tab) => tab.id === stored.activeTab)) throw new Error('invalid active tab')
    const expandedItems = stored.expandedItems === undefined ? null
      : Array.isArray(stored.expandedItems) && stored.expandedItems.every((id) => typeof id === 'string') ? [...new Set(stored.expandedItems)] : null
    const knownWorkspaceItems = stored.knownWorkspaceItems === undefined ? null
      : Array.isArray(stored.knownWorkspaceItems) && stored.knownWorkspaceItems.every((id) => typeof id === 'string') ? [...new Set(stored.knownWorkspaceItems)] : null
    return { tabs, activeTab: stored.activeTab, sidebarWidth: clampSidebarWidth(stored.sidebarWidth), expandedItems, knownWorkspaceItems }
  } catch {
    return { tabs: [boardTab], activeTab: boardTab.id, sidebarWidth: defaultSidebarWidth, expandedItems: null, knownWorkspaceItems: null }
  }
}

function Shell({ initialRoute }: { initialRoute: Exclude<Route, { page: 'missing' }> }) {
  const [initial] = useState(() => {
    const layout = readLayout()
    const storedTabs = createTabState(
      layout.tabs.flatMap((tab) => tab.kind === 'agent' ? [tab.name] : []),
      layout.activeTab,
    )
    return { ...layout, tabState: applyRoute(storedTabs, initialRoute) }
  })
  const [tabState, setTabState] = useState<AgentTabState>(initial.tabState)
  const [screenTabs, setScreenTabs] = useState<Array<{ pane: Pane, preview: boolean }>>([])
  const [sidebarWidth, setSidebarWidth] = useState(initial.sidebarWidth)
  const [expandedItems, setExpandedItems] = useState<string[] | null>(initial.expandedItems)
  const [knownWorkspaceItems, setKnownWorkspaceItems] = useState<string[] | null>(initial.knownWorkspaceItems)
  const [lifecycleProblems, setLifecycleProblems] = useState<Record<string, string>>({})
  const [agentStatuses, setAgentStatuses] = useState<Record<string, string>>({})
  const tabRefs = useRef(new Map<string, HTMLButtonElement>())
  const queryClient = useQueryClient()
  const tabs: ShellTab[] = [boardTab, ...tabState.tabs.map((tab) => agentTab(tab.name, tab.preview)), ...screenTabs.map((tab) => screenTab(tab.pane, tab.preview))]
  const activeTab = tabState.activeTab
  const agentNames = tabState.tabs.map((tab) => tab.name)
  const screenPaneIDs = screenTabs.map((tab) => tab.pane.pane_id)
  const boardQuery = useQuery({ queryKey: queryKeys.fleet, queryFn: () => getFleet(), staleTime: Infinity, retry: false })
  const viewerQuery = useQuery(viewerQueryOptions())
  const stream = useFleetStream(agentNames, screenPaneIDs)
  const active = tabs.find((tab) => tab.id === activeTab) ?? boardTab
  const viewerFailure = viewerQuery.error ? apiProblem(viewerQuery.error) : null
  const viewer = viewerQuery.data?.viewer ?? 'unresolved'
  const viewerState = viewerQuery.isPending ? 'resolving' : viewerQuery.data ? 'attributed' : viewerFailure?.response?.status === 409 ? 'unresolved' : 'unavailable'
  const viewerProblem = viewerState === 'unavailable' ? viewerFailure?.problem.detail ?? '' : ''
  const viewerReadOnly = viewerQuery.isPending ? 'Resolving viewer identity…'
    : viewerQuery.data ? ''
      : viewerReadOnlyMessage(viewerFailure?.problem ?? { error: 'request failed', detail: 'unknown failure' }, viewerFailure?.response?.status)

  const setPath = useCallback((tab: ShellTab, push = true) => {
    if (tab.kind === 'screen') return
    const path = tab.kind === 'board' ? '/' : `/agents/${encodeURIComponent(tab.name)}`
    if (push && window.location.pathname !== path) window.history.pushState({}, '', path)
  }, [])

  const activate = useCallback((tab: ShellTab, push = true) => {
    setTabState((current) => tab.kind === 'board'
      ? { ...current, activeTab: boardTab.id }
      : tab.kind === 'screen' ? { ...current, activeTab: tab.id }
        : current.tabs.some((item) => item.name === tab.name)
          ? { ...current, activeTab: tab.id }
          : previewAgent(current, tab.name))
    setPath(tab, push)
  }, [setPath])

  const previewPane = useCallback((pane: Pane) => {
    setScreenTabs((current) => {
      if (current.some((tab) => tab.pane.pane_id === pane.pane_id)) return current
      const previewIndex = current.findIndex((tab) => tab.preview)
      const next = [...current]
      if (previewIndex === -1) next.push({ pane, preview: true }); else next[previewIndex] = { pane, preview: true }
      return next
    })
    setTabState((current) => ({ ...current, activeTab: screenTabID(pane.pane_id) }))
  }, [])

  const pinPane = useCallback((pane: Pane) => {
    setScreenTabs((current) => current.some((tab) => tab.pane.pane_id === pane.pane_id)
      ? current.map((tab) => tab.pane.pane_id === pane.pane_id ? { pane, preview: false } : tab)
      : [...current, { pane, preview: false }])
    setTabState((current) => ({ ...current, activeTab: screenTabID(pane.pane_id) }))
  }, [])

  const preview = useCallback((name: string) => {
    setTabState((current) => previewAgent(current, name))
    setPath(agentTab(name))
  }, [setPath])

  const pin = useCallback((name: string) => {
    setTabState((current) => pinAgent(current, name))
    setPath(agentTab(name))
  }, [setPath])

  useEffect(() => {
    const update = () => {
      const route = currentRoute()
      if (route.page !== 'missing') setTabState((current) => applyRoute(current, route))
    }
    window.addEventListener('popstate', update)
    return () => window.removeEventListener('popstate', update)
  }, [])

  useEffect(() => {
    const pinnedAgents = storedPinnedAgents(tabState)
    // Preview tabs are deliberately session-ephemeral; only pinned tabs enter browser layout storage.
    const persistedActiveTab = tabState.activeTab === boardTab.id || pinnedAgents.some((name) => agentTabID(name) === tabState.activeTab)
      ? tabState.activeTab
      : boardTab.id
    const value: StoredLayout = {
      openTabs: pinnedAgents,
      activeTab: persistedActiveTab,
      sidebarWidth,
    }
    if (expandedItems !== null) value.expandedItems = expandedItems
    if (knownWorkspaceItems !== null) value.knownWorkspaceItems = knownWorkspaceItems
    try { localStorage.setItem(layoutKey, JSON.stringify(value)) } catch { /* best effort */ }
  }, [tabState, sidebarWidth, expandedItems, knownWorkspaceItems])

  const close = useCallback((id: string) => {
    const index = tabs.findIndex((tab) => tab.id === id)
    const nextTabs = tabs.filter((tab) => tab.id !== id)
    const next = nextTabs[Math.max(0, index - 1)] ?? boardTab
    setTabState((current) => ({
      tabs: current.tabs.filter((tab) => agentTabID(tab.name) !== id),
      activeTab: current.activeTab === id ? next.id : current.activeTab,
    }))
    setScreenTabs((current) => current.filter((tab) => screenTabID(tab.pane.pane_id) !== id))
    if (id.startsWith('agent:')) setAgentStatuses((current) => {
      const next = { ...current }
      delete next[id.slice('agent:'.length)]
      return next
    })
    if (activeTab === id) setPath(next)
  }, [activeTab, setPath, tabs])

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      const command = event.ctrlKey || event.metaKey
      if (command && event.key.toLowerCase() === 'w' && active.kind !== 'board') close(active.id)
      else if (command && (event.key === 'PageDown' || event.key === 'PageUp')) {
        const index = tabs.findIndex((tab) => tab.id === active.id)
        activate(tabs[(index + (event.key === 'PageDown' ? 1 : -1) + tabs.length) % tabs.length])
      } else if (event.altKey && event.key === '1') document.querySelector<HTMLElement>('.fleet-tree [role="treeitem"]')?.focus()
      else if (event.altKey && event.key === '2') document.querySelector<HTMLTextAreaElement>('.hosted-panel:not([hidden]) textarea[data-composer]')?.focus()
      else return
      event.preventDefault()
    }
    window.addEventListener('keydown', shortcut)
    return () => window.removeEventListener('keydown', shortcut)
  }, [active, activate, close, tabs])

  const startResize = (event: React.PointerEvent) => {
    const startX = event.clientX
    const startWidth = sidebarWidth
    const move = (moveEvent: PointerEvent) => setSidebarWidth(clampSidebarWidth(startWidth + moveEvent.clientX - startX))
    const stop = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop) }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop)
  }

  const setLifecycleBanner = useCallback((key: string, detail: string) => setLifecycleProblems((current) => {
    const next = { ...current }
    if (detail) next[key] = detail
    else delete next[key]
    return next
  }), [])
  const setAgentStatus = useCallback((name: string, status: string) => setAgentStatuses((current) => current[name] === status ? current : { ...current, [name]: status }), [])
  const streamProblems: Record<string, string> = { ...stream.problems, ...(boardQuery.error ? { fleet: boardQuery.error.message } : {}), ...lifecycleProblems }

  return <div className="app-shell">
    <div className="sidebar-region" style={{ width: sidebarWidth }}>
      <FleetSidebar board={boardQuery.data} activeAgent={active.kind === 'agent' ? active.name : undefined} activePane={active.kind === 'screen' ? active.pane.pane_id : undefined} onPreviewAgent={preview} onPinAgent={pin} onPreviewPane={previewPane} onPinPane={pinPane}
        expandedItems={expandedItems} onExpandedItems={setExpandedItems} knownWorkspaceItems={knownWorkspaceItems} onKnownWorkspaceItems={setKnownWorkspaceItems} />
    </div>
    <div className="sidebar-resizer" role="separator" aria-label="Resize fleet sidebar" aria-orientation="vertical" aria-valuemin={200} aria-valuemax={440} aria-valuenow={sidebarWidth} tabIndex={0}
      onPointerDown={startResize} onKeyDown={(event) => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
        setSidebarWidth((width) => clampSidebarWidth(width + (event.key === 'ArrowRight' ? 10 : -10)))
        event.preventDefault()
      }} />
    <section className="shell-main">
      <div className="tab-strip" role="tablist" aria-label="Open panels">
        {tabs.map((tab, index) => {
          const boardStatus = tab.kind === 'agent' ? agentBusStatus(boardQuery.data, tab.name) : '-'
          const liveStatus = boardStatus !== '-' || tab.kind !== 'agent' ? boardStatus : agentStatuses[tab.name] ?? '-'
          return <div role="presentation" className={`shell-tab${tab.id === activeTab ? ' active' : ''}${tab.kind !== 'board' && tab.preview ? ' preview' : ''}`} key={tab.id} onAuxClick={(event) => { if (event.button === 1 && tab.kind !== 'board') close(tab.id) }}>
          <button ref={(node) => { if (node) tabRefs.current.set(tab.id, node); else tabRefs.current.delete(tab.id) }} id={`shell-tab-${index}`} aria-controls={`shell-panel-${index}`} role="tab" aria-selected={tab.id === activeTab} tabIndex={tab.id === activeTab ? 0 : -1}
            title={tab.kind !== 'board' && tab.preview ? 'Preview — double-click to pin' : undefined}
            onClick={() => activate(tab)} onDoubleClick={() => { if (tab.kind === 'agent' && tab.preview) pin(tab.name); else if (tab.kind === 'screen' && tab.preview) pinPane(tab.pane) }} onKeyDown={(event) => {
              let target = index
              if (event.key === 'ArrowLeft') target = (index - 1 + tabs.length) % tabs.length
              else if (event.key === 'ArrowRight') target = (index + 1) % tabs.length
              else if (event.key === 'Home') target = 0
              else if (event.key === 'End') target = tabs.length - 1
              else return
              activate(tabs[target])
              requestAnimationFrame(() => tabRefs.current.get(tabs[target].id)?.focus())
              event.preventDefault()
            }}>{tab.kind === 'board' ? '⌗ Board' : tab.kind === 'screen' ? <><span className="tab-label">▣ {tab.label}</span><span className="tab-agent-status">read-only</span></> : <><span className="tab-label">{tab.label}</span><span className="tab-agent-status"><AgentStatusDot status={liveStatus} />{liveStatus !== '-' ? liveStatus : 'unknown'}</span></>}</button>
          {tab.kind !== 'board' && <button className="close-tab" aria-label={`Close ${tab.label}`} onClick={() => close(tab.id)}>×</button>}
        </div>})}
        <button className="new-tab" type="button" title="Open agents and terminal panes from the fleet sidebar" aria-label="Open an agent or terminal pane from the fleet sidebar">+</button>
        <span className="tab-strip-spacer" />
        <span className={`stream-chip${streamProblems.stream ? ' fault' : ''}`}>{streamProblems.stream ? 'SSE: reconnecting' : 'SSE: connected'}</span>
        <span className="layout-chip" title="Shortcuts: Ctrl/Cmd+W close tab · Ctrl/Cmd+PageUp/PageDown previous/next tab · Alt+1 focus sidebar · Alt+2 focus composer">layout: this browser</span>
        <ThemeToggle />
      </div>
      <div className="shell-banners">
        {stream.serverUpdated && <div className="banner server-update" role="alert"><strong>update</strong><span>Server updated — refresh to load the new version</span><button type="button" onClick={() => window.location.reload()}>Refresh</button></div>}
        {viewerProblem && <Banner source="viewer" detail={viewerProblem} />}{Object.entries(streamProblems).map(([source, detail]) => <Banner source={source} detail={detail} tone={source === 'stream' && detail === 'Connecting to live fleet…' ? 'info' : 'error'} key={source} />)}
      </div>
      <div className="panel-host">
        {tabs.map((tab, index) => <div id={`shell-panel-${index}`} role="tabpanel" aria-labelledby={`shell-tab-${index}`} hidden={tab.id !== activeTab} className="hosted-panel" key={tab.id}>
          {tab.kind === 'board' ? <BoardPanel board={boardQuery.data} onBanner={setLifecycleBanner} /> : tab.kind === 'screen' ? <ScreenPanel pane={tab.pane} /> : <AgentPanel name={tab.name} liveStatus={agentBusStatus(boardQuery.data, tab.name)} identityReadOnly={viewerReadOnly} onViewer={(resolvedViewer) => queryClient.setQueryData(queryKeys.viewer, { viewer: resolvedViewer })} onSend={() => setTabState((current) => autoPinPreview(current, tab.name))} onStatus={setAgentStatus} />}
        </div>)}
      </div>
      <footer className="status-bar">
        <span>substrate: herdr {boardQuery.data ? '✓' : '…'} · hcom {streamProblems.hcom ? '×' : '✓'}</span>
        <span className={streamProblems.stream ? 'fault' : ''}>SSE: {streamProblems.stream ? 'reconnecting' : 'connected'}</span>
        <span>viewer: {viewerQuery.isPending ? 'resolving…' : viewer}</span><span>{viewerState === 'resolving' ? 'resolving identity' : viewerState === 'attributed' ? 'attributed' : viewerState === 'unavailable' ? 'identity unavailable' : 'read-only · unattributed'}</span>
        <span className="status-spacer" /><span>{stream.messages} messages</span><span>last event: {stream.lastEvent ? new Date(stream.lastEvent).toLocaleTimeString() : '—'}</span>
      </footer>
    </section>
  </div>
}

export default function App() {
  const route = currentRoute()
  if (route.page !== 'missing') return <Shell initialRoute={route} />
  return <main className="agent-page"><AppLink to="/" className="back-link">← Fleet board</AppLink><section className="not-found"><strong>404 · Page not found</strong></section></main>
}
