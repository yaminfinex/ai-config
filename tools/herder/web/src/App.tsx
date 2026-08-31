import { DockviewReact, type DockviewTheme } from 'dockview-react'
import { FleetSidebar } from './features/sidebar/FleetSidebar'
import { QuickOpen } from './features/files/QuickOpen'
import { ShortcutReference } from './features/layout/ShortcutReference'
import { clampSidebarWidth } from './features/layout/useLayoutPersistence'
import { dockComponents, DockTab } from './features/workspace/panelRegistry'
import { DockHeaderActions, DockWatermark } from './features/workspace/workspaceChrome'
import { WorkspaceProviders } from './features/workspace/workspaceContext'
import { useWorkspaceController } from './features/workspace/useWorkspaceController'
import { FileWatchContext } from './stream/fileWatchRegistry'
import { AppLink, currentRoute, type Route } from './shared/navigation'
import { Banner } from './shared/presentation'
import { statusBarHealth, type HealthTick } from './shared/statusBarPresentation'
import { ThemeToggle } from './shared/ThemeToggle'

const herderTheme: DockviewTheme = {
  name: 'herder', className: 'dockview-theme-herder', gap: 0,
  dndOverlayMounting: 'absolute', dndPanelOverlay: 'group', dndTabIndicator: 'line',
  dndOverlayBorder: '2px solid var(--accent)', tabGroupIndicator: 'none', tabAnimation: 'smooth',
}

function StatusTick({ tick }: { tick: HealthTick }) {
  return <span className="health-tick" title={tick.title} aria-label={tick.title}><span className={`health-dot ${tick.healthy ? 'healthy' : 'fault'}`} aria-hidden="true" />{tick.label}</span>
}

function Shell({ initialRoute }: { initialRoute: Exclude<Route, { page: 'missing' }> }) {
  const workspace = useWorkspaceController(initialRoute)
  const {
    openAgent, openScreen, openFile, openFolder,
    sidebarWidth, setSidebarWidth, expandedItems, setExpandedItems, knownWorkspaceItems, setKnownWorkspaceItems,
  } = workspace
  const lastEvent = workspace.stream.lastEvent ? new Date(workspace.stream.lastEvent).toLocaleTimeString() : '—'
  const health = statusBarHealth({ problems: workspace.streamProblems, substrateProof: workspace.stream.substrateProof, lastEventLabel: lastEvent })

  return <WorkspaceProviders actions={workspace.actions} data={workspace.data}><FileWatchContext.Provider value={workspace.fileWatchRegister}><div className="app-shell">
    <QuickOpen open={workspace.quickOpen} agent={workspace.quickOpenAgent} groupID={workspace.quickOpenGroup} onClose={workspace.closeQuickOpen} onOpenFile={openFile} onOpenFolder={openFolder} />
    <ShortcutReference open={workspace.shortcutReference} onClose={() => workspace.setShortcutReference(false)} />
    <div className="sidebar-region" style={{ width: sidebarWidth }}>
      <FleetSidebar board={workspace.board} activeAgent={workspace.activeAgent} activePane={workspace.activePane}
        onPreviewAgent={(name, placement) => openAgent(name, true, placement, true)} onPinAgent={(name, placement) => openAgent(name, false, placement, true)} onPreviewPane={(pane, placement) => openScreen(pane, true, placement)} onPinPane={(pane, placement) => openScreen(pane, false, placement)}
        expandedItems={expandedItems} onExpandedItems={setExpandedItems} knownWorkspaceItems={knownWorkspaceItems} onKnownWorkspaceItems={setKnownWorkspaceItems} />
    </div>
    <div className="sidebar-resizer" role="separator" aria-label="Resize fleet sidebar" aria-orientation="vertical" aria-valuemin={200} aria-valuemax={440} aria-valuenow={sidebarWidth} tabIndex={0}
      onPointerDown={workspace.startResize} onKeyDown={(event) => { if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { setSidebarWidth((width) => clampSidebarWidth(width + (event.key === 'ArrowRight' ? 10 : -10))); event.preventDefault() } }} />
    <section className="shell-main">
      <div className="shell-banners">
        {workspace.stream.serverUpdated && <div className="banner server-update" role="alert"><strong>update</strong><span>Server updated — refresh to load the new version</span><button type="button" onClick={() => { workspace.flushLayout(); window.location.reload() }}>Refresh</button></div>}
        {workspace.viewerProblem && <Banner source="viewer" detail={workspace.viewerProblem} />}{Object.entries(workspace.streamProblems).map(([source, detail]) => <Banner source={source} detail={detail} tone={source === 'stream' && detail === 'Connecting to live fleet…' ? 'info' : 'error'} key={source} />)}
      </div>
      <div className="dock-host">
        <DockviewReact components={dockComponents} tabComponents={{ 'herder-tab': DockTab }} rightHeaderActionsComponent={DockHeaderActions} watermarkComponent={DockWatermark}
          onReady={workspace.onDockReady} theme={herderTheme} disableFloatingGroups announcements noPanelsOverlay="watermark" tabGroupAccent="off"
          pinnedTabs={{ enabled: false }} layoutHistory={{ enabled: false }} autoHideEdgeGroups={false} dockToEdgeGroups={false} dndCompass={false} />
      </div>
      <footer className="status-bar">
        {health.map((tick) => <StatusTick tick={tick} key={tick.label} />)}
        <span title="Web sends are attributed to this user; web senders are not addressable bus peers.">user: {workspace.viewerPending ? 'resolving…' : workspace.viewer}</span>
        <span className="status-spacer" /><span>last event: {lastEvent}</span>
        <button type="button" className="shortcut-button" title="Keyboard shortcuts (?)" aria-label="Open keyboard shortcuts" onClick={() => workspace.setShortcutReference(true)}>?</button>
        <ThemeToggle />
      </footer>
    </section>
  </div></FileWatchContext.Provider></WorkspaceProviders>
}

export default function App() {
  const route = currentRoute()
  if (route.page !== 'missing') return <Shell initialRoute={route} />
  return <main className="agent-page"><AppLink to="/" className="back-link">← Workspace</AppLink><section className="not-found"><strong>404 · Page not found</strong></section></main>
}
