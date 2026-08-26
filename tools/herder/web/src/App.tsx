import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiProblem, getFleet, queryKeys } from './api/client'
import { viewerQueryOptions } from './api/queries'
import { BoardPanel } from './features/board/BoardPanel'
import { FleetSidebar } from './features/sidebar/FleetSidebar'
import { AgentPanel } from './features/transcript/AgentPanel'
import { AppLink, currentRoute, type Route } from './shared/navigation'
import { Banner } from './shared/presentation'
import { useFleetStream } from './stream/useFleetStream'

const layoutKey = 'herder.web.layout.v1'
const boardTab = { id: 'board', kind: 'board' as const, label: 'Board' }
const defaultSidebarWidth = 250

type ShellTab = typeof boardTab | { id: string, kind: 'agent', label: string, name: string }
type StoredLayout = {
  openTabs: string[]
  activeTab: string
  sidebarWidth: number
  expandedItems?: string[]
  knownWorkspaceItems?: string[]
}

function agentTab(name: string): ShellTab {
  return { id: `agent:${name}`, kind: 'agent', label: name, name }
}

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
    const tabs = [boardTab, ...[...new Set(stored.openTabs)].map(agentTab)]
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
    if (initialRoute.page === 'agent') {
      const tab = agentTab(initialRoute.name)
      if (!layout.tabs.some((item) => item.id === tab.id)) layout.tabs.push(tab)
      layout.activeTab = tab.id
    } else layout.activeTab = boardTab.id
    return layout
  })
  const [tabs, setTabs] = useState(initial.tabs)
  const [activeTab, setActiveTab] = useState(initial.activeTab)
  const [sidebarWidth, setSidebarWidth] = useState(initial.sidebarWidth)
  const [expandedItems, setExpandedItems] = useState<string[] | null>(initial.expandedItems)
  const [knownWorkspaceItems, setKnownWorkspaceItems] = useState<string[] | null>(initial.knownWorkspaceItems)
  const [lifecycleProblems, setLifecycleProblems] = useState<Record<string, string>>({})
  const tabRefs = useRef(new Map<string, HTMLButtonElement>())
  const queryClient = useQueryClient()
  const agentNames = tabs.flatMap((tab) => tab.kind === 'agent' ? [tab.name] : [])
  const boardQuery = useQuery({ queryKey: queryKeys.fleet, queryFn: () => getFleet(), staleTime: Infinity, retry: false })
  const viewerQuery = useQuery(viewerQueryOptions())
  const stream = useFleetStream(agentNames)
  const active = tabs.find((tab) => tab.id === activeTab) ?? boardTab
  const viewerFailure = viewerQuery.error ? apiProblem(viewerQuery.error) : null
  const viewer = viewerQuery.data?.viewer ?? 'unresolved'
  const viewerState = viewerQuery.isPending ? 'resolving' : viewerQuery.data ? 'attributed' : viewerFailure?.response?.status === 409 ? 'unresolved' : 'unavailable'
  const viewerProblem = viewerState === 'unavailable' ? viewerFailure?.problem.detail ?? '' : ''
  const viewerReadOnly = viewerQuery.isPending ? 'Resolving viewer identity…'
    : viewerQuery.data ? ''
      : viewerFailure?.response?.status === 409 ? `Connect via Tailscale to send. ${viewerFailure.problem.detail}`
        : `Viewer identity is unavailable. ${viewerFailure?.problem.detail ?? 'unknown failure'}`

  const activate = useCallback((tab: ShellTab, push = true) => {
    setTabs((current) => current.some((item) => item.id === tab.id) ? current : [...current, tab])
    setActiveTab(tab.id)
    const path = tab.kind === 'board' ? '/' : `/agents/${encodeURIComponent(tab.name)}`
    if (push && window.location.pathname !== path) window.history.pushState({}, '', path)
  }, [])

  useEffect(() => {
    const update = () => {
      const route = currentRoute()
      if (route.page === 'board') activate(boardTab, false)
      else if (route.page === 'agent') activate(agentTab(route.name), false)
    }
    window.addEventListener('popstate', update)
    return () => window.removeEventListener('popstate', update)
  }, [activate])

  useEffect(() => {
    const value: StoredLayout = {
      openTabs: tabs.flatMap((tab) => tab.kind === 'agent' ? [tab.name] : []),
      activeTab,
      sidebarWidth,
    }
    if (expandedItems !== null) value.expandedItems = expandedItems
    if (knownWorkspaceItems !== null) value.knownWorkspaceItems = knownWorkspaceItems
    try { localStorage.setItem(layoutKey, JSON.stringify(value)) } catch { /* best effort */ }
  }, [tabs, activeTab, sidebarWidth, expandedItems, knownWorkspaceItems])

  const close = useCallback((id: string) => {
    const index = tabs.findIndex((tab) => tab.id === id)
    const nextTabs = tabs.filter((tab) => tab.id !== id)
    setTabs(nextTabs)
    if (activeTab === id) activate(nextTabs[Math.max(0, index - 1)] ?? boardTab)
  }, [activeTab, activate, tabs])

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      const command = event.ctrlKey || event.metaKey
      if (command && event.key.toLowerCase() === 'w' && active.kind === 'agent') close(active.id)
      else if (command && (event.key === 'PageDown' || event.key === 'PageUp')) {
        const index = tabs.findIndex((tab) => tab.id === active.id)
        activate(tabs[(index + (event.key === 'PageDown' ? 1 : -1) + tabs.length) % tabs.length])
      } else if (event.altKey && event.key === '1') document.querySelector<HTMLElement>('.fleet-tree [role="treeitem"]')?.focus()
      else if (event.altKey && event.key === '2') document.querySelector<HTMLTextAreaElement>('.hosted-panel:not([hidden]) #message')?.focus()
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
  const streamProblems: Record<string, string> = { ...stream.problems, ...(boardQuery.error ? { fleet: boardQuery.error.message } : {}), ...lifecycleProblems }

  return <div className="app-shell">
    <div className="sidebar-region" style={{ width: sidebarWidth }}>
      <FleetSidebar board={boardQuery.data} activeAgent={active.kind === 'agent' ? active.name : undefined} onOpenAgent={(name) => activate(agentTab(name))}
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
        {tabs.map((tab, index) => <div role="presentation" className={`shell-tab${tab.id === activeTab ? ' active' : ''}`} key={tab.id} onAuxClick={(event) => { if (event.button === 1 && tab.kind === 'agent') close(tab.id) }}>
          <button ref={(node) => { if (node) tabRefs.current.set(tab.id, node); else tabRefs.current.delete(tab.id) }} id={`shell-tab-${index}`} aria-controls={`shell-panel-${index}`} role="tab" aria-selected={tab.id === activeTab} tabIndex={tab.id === activeTab ? 0 : -1}
            onClick={() => activate(tab)} onKeyDown={(event) => {
              let target = index
              if (event.key === 'ArrowLeft') target = (index - 1 + tabs.length) % tabs.length
              else if (event.key === 'ArrowRight') target = (index + 1) % tabs.length
              else if (event.key === 'Home') target = 0
              else if (event.key === 'End') target = tabs.length - 1
              else return
              activate(tabs[target])
              requestAnimationFrame(() => tabRefs.current.get(tabs[target].id)?.focus())
              event.preventDefault()
            }}>{tab.kind === 'board' ? '⌗ Board' : tab.label}</button>
          {tab.kind === 'agent' && <button className="close-tab" aria-label={`Close ${tab.label}`} onClick={() => close(tab.id)}>×</button>}
        </div>)}
        <button className="new-tab" type="button" title="Open agents from the fleet sidebar" aria-label="Open an agent from the fleet sidebar">+</button>
        <span className="tab-strip-spacer" />
        <span className={`stream-chip${streamProblems.stream ? ' fault' : ''}`}>{streamProblems.stream ? 'SSE: reconnecting' : 'SSE: connected'}</span>
        <span className="layout-chip" title="Shortcuts: Ctrl/Cmd+W close tab · Ctrl/Cmd+PageUp/PageDown previous/next tab · Alt+1 focus sidebar · Alt+2 focus composer">layout: this browser</span>
      </div>
      <div className="shell-banners">{viewerProblem && <Banner source="viewer" detail={viewerProblem} />}{Object.entries(streamProblems).map(([source, detail]) => <Banner source={source} detail={detail} key={source} />)}</div>
      <div className="panel-host">
        {tabs.map((tab, index) => <div id={`shell-panel-${index}`} role="tabpanel" aria-labelledby={`shell-tab-${index}`} hidden={tab.id !== activeTab} className="hosted-panel" key={tab.id}>
          {tab.kind === 'board' ? <BoardPanel board={boardQuery.data} onBanner={setLifecycleBanner} /> : <AgentPanel name={tab.name} identityReadOnly={viewerReadOnly} onViewer={(resolvedViewer) => queryClient.setQueryData(queryKeys.viewer, { viewer: resolvedViewer })} />}
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
