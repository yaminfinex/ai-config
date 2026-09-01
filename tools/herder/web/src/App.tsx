import { useMemo, type ReactNode } from 'react'
import { DockviewReact, type DockviewTheme } from 'dockview-react'
import { FleetSidebar } from './features/sidebar/FleetSidebar'
import { QuickOpen } from './features/files/QuickOpen'
import { ShortcutReference } from './features/layout/ShortcutReference'
import { RailStatusToggle, UtilityRail } from './features/layout/UtilityRail'
import { dockComponents, DockTab } from './features/workspace/panelRegistry'
import { DockHeaderActions, DockWatermark } from './features/workspace/workspaceChrome'
import { WorkspaceProviders } from './features/workspace/workspaceContext'
import { useWorkspaceController } from './features/workspace/useWorkspaceController'
import { FileWatchContext } from './stream/fileWatchRegistry'
import { useStreamAlerts, useStreamStatus } from './stream/useFleetStream'
import { AppLink, currentRoute, type Route } from './shared/navigation'
import { Banner } from './shared/presentation'
import { statusBarHealth, type HealthTick } from './shared/statusBarPresentation'
import { ThemeToggle } from './shared/ThemeToggle'
import { NotesProvider, useNotes } from './features/notes/NotesProvider'
import { NotesRail } from './features/notes/NotesRail'
import { shortcutLabels } from './features/layout/shellShortcuts'
import { defaultMaxSpaces, SpaceStrip } from './features/spaces/index.ts'
import { liveRosterNames } from './features/notes/notesPresentation.ts'

const herderTheme: DockviewTheme = {
  name: 'herder', className: 'dockview-theme-herder', gap: 0,
  dndOverlayMounting: 'absolute', dndPanelOverlay: 'group', dndTabIndicator: 'line',
  dndOverlayBorder: '2px solid var(--accent)', tabGroupIndicator: 'none', tabAnimation: 'smooth',
}

function StatusTick({ tick }: { tick: HealthTick }) {
  return <span className="health-tick" title={tick.title} aria-label={tick.title}><span className={`health-dot ${tick.healthy ? 'healthy' : 'fault'}`} aria-hidden="true" />{tick.label}</span>
}

function NotesCount() {
  const { notes } = useNotes()
  return <span title="Notes saved in this browser">notes: {notes.length}</span>
}

function streamProblems(problems: Record<string, string>, fleetProblem: string) {
  return { ...problems, ...(fleetProblem ? { fleet: fleetProblem } : {}) }
}

function StreamBanners({ fleetProblem, viewerProblem, spaceProblem, flushLayout }: { fleetProblem: string, viewerProblem: string, spaceProblem: string, flushLayout: () => void }) {
  const stream = useStreamAlerts()
  const problems = useMemo(() => streamProblems(stream.problems, fleetProblem), [fleetProblem, stream.problems])
  return <div className="shell-banners">
    {stream.serverUpdated && <div className="banner server-update" role="alert"><strong>update</strong><span>Server updated — refresh to load the new version</span><button type="button" onClick={() => { flushLayout(); window.location.reload() }}>Refresh</button></div>}
    {viewerProblem && <Banner source="viewer" detail={viewerProblem} />}{spaceProblem && <Banner source="space" detail={spaceProblem} />}{Object.entries(problems).map(([source, detail]) => <Banner source={source} detail={detail} tone={source === 'stream' && detail === 'Connecting to live fleet…' ? 'info' : 'error'} key={source} />)}
  </div>
}

function StreamStatusBar({ fleetProblem, viewer, viewerPending, fleetCollapsed, notesCollapsed, onToggleFleet, onToggleNotes, onShortcuts, spaceStrip }: {
  fleetProblem: string
  viewer: string
  viewerPending: boolean
  fleetCollapsed: boolean
  notesCollapsed: boolean
  onToggleFleet: () => void
  onToggleNotes: () => void
  onShortcuts: () => void
  spaceStrip: ReactNode
}) {
  const stream = useStreamStatus()
  const problems = useMemo(() => streamProblems(stream.problems, fleetProblem), [fleetProblem, stream.problems])
  const lastEvent = stream.lastEvent ? new Date(stream.lastEvent).toLocaleTimeString() : '—'
  const health = statusBarHealth({ problems, substrateProof: stream.substrateProof, lastEventLabel: lastEvent })
  const shortcuts = shortcutLabels(navigator.userAgent)
  return <footer className="status-bar">
    <div className="status-primary">
      <RailStatusToggle side="left" label="Fleet" shortcut={shortcuts.focusFleet} collapsed={fleetCollapsed} onToggle={onToggleFleet} />
      <div className="workspace-switcher-slot">{spaceStrip}</div>
    </div>
    <div className="status-secondary">
      <span className="status-health">{health.map((tick) => <StatusTick tick={tick} key={tick.label} />)}</span>
      <span className="status-user" title="Web sends are attributed to this user; web senders are not addressable bus peers.">user: {viewerPending ? 'resolving…' : viewer}</span>
      <span className="status-notes-count"><NotesCount /></span>
      <span className="status-last">last event: {lastEvent}</span>
      <button type="button" className="shortcut-button" title="Keyboard shortcuts (?)" aria-label="Open keyboard shortcuts" onClick={onShortcuts}>?</button>
      <ThemeToggle />
      <RailStatusToggle side="right" label="Notes" shortcut={shortcuts.toggleNotesRail} collapsed={notesCollapsed} onToggle={onToggleNotes} />
    </div>
  </footer>
}

function Shell({ initialRoute }: { initialRoute: Exclude<Route, { page: 'missing' }> }) {
  const workspace = useWorkspaceController(initialRoute)
  const {
    openAgent, openScreen, openFile, openFolder,
    fleetRail, setFleetRail, notesRail, setNotesRail, expandedItems, setExpandedItems, knownWorkspaceItems, setKnownWorkspaceItems,
  } = workspace
  return <WorkspaceProviders actions={workspace.actions} data={workspace.data}><FileWatchContext.Provider value={workspace.fileWatchRegister}><div className="app-shell">
    <QuickOpen open={workspace.quickOpen} agent={workspace.quickOpenAgent} groupID={workspace.quickOpenGroup}
      spaces={workspace.spaces.items} activeSpaceID={workspace.spaces.activeID} agents={liveRosterNames(workspace.board)} atSpaceCap={workspace.spaces.items.length >= defaultMaxSpaces}
      onClose={workspace.closeQuickOpen} onOpenFile={openFile} onOpenFolder={openFolder}
      onOpenAgent={(name) => openAgent(name, true, undefined, true)} onSwitchSpace={workspace.spaces.switch} onCreateSpace={workspace.spaces.createNamed} />
    <ShortcutReference open={workspace.shortcutReference} onClose={() => workspace.setShortcutReference(false)} />
    <UtilityRail side="left" label="Fleet" detail="herdr truth" headingStart={<span className="status-dot listening" />}
      width={fleetRail.width} collapsed={fleetRail.collapsed}
      onWidth={(width) => setFleetRail((rail) => ({ ...rail, width }))} onToggle={workspace.toggleFleetRail}>
      <FleetSidebar board={workspace.board} activeAgent={workspace.activeAgent} activePane={workspace.activePane}
        onPreviewAgent={(name, placement) => openAgent(name, true, placement, true)} onPinAgent={(name, placement) => openAgent(name, false, placement, true)} onPreviewPane={(pane, placement) => openScreen(pane, true, placement)} onPinPane={(pane, placement) => openScreen(pane, false, placement)}
        expandedItems={expandedItems} onExpandedItems={setExpandedItems} knownWorkspaceItems={knownWorkspaceItems} onKnownWorkspaceItems={setKnownWorkspaceItems} />
    </UtilityRail>
    <section className="shell-main">
      <StreamBanners fleetProblem={workspace.fleetProblem} viewerProblem={workspace.viewerProblem} spaceProblem={workspace.spaces.enabled ? workspace.spaceProblem : ''} flushLayout={workspace.flushLayout} />
      <div className="dock-host">
        <DockviewReact components={dockComponents} tabComponents={{ 'herder-tab': DockTab }} rightHeaderActionsComponent={DockHeaderActions} watermarkComponent={DockWatermark}
          onReady={workspace.onDockReady} theme={herderTheme} disableFloatingGroups announcements noPanelsOverlay="watermark" tabGroupAccent="off"
          pinnedTabs={{ enabled: false }} layoutHistory={{ enabled: false }} autoHideEdgeGroups={false} dockToEdgeGroups={false} dndCompass={false} />
      </div>
      <StreamStatusBar fleetProblem={workspace.fleetProblem} viewer={workspace.viewer} viewerPending={workspace.viewerPending}
        fleetCollapsed={fleetRail.collapsed} notesCollapsed={notesRail.collapsed}
        onToggleFleet={workspace.toggleFleetRail} onToggleNotes={workspace.toggleNotesRail}
        onShortcuts={() => workspace.setShortcutReference(true)}
        spaceStrip={<SpaceStrip {...workspace.spaces} />} />
    </section>
    <UtilityRail side="right" label="Notes" width={notesRail.width} collapsed={notesRail.collapsed}
      onWidth={(width) => setNotesRail((rail) => ({ ...rail, width }))} onToggle={workspace.toggleNotesRail}>
      <NotesRail board={workspace.board} onOpenAgent={(name, placement) => openAgent(name, true, placement, true)} />
    </UtilityRail>
  </div></FileWatchContext.Provider></WorkspaceProviders>
}

export default function App() {
  const route = currentRoute()
  if (route.page !== 'missing') return <NotesProvider><Shell initialRoute={route} /></NotesProvider>
  return <main className="agent-page"><AppLink to="/" className="back-link">← Workspace</AppLink><section className="not-found"><strong>404 · Page not found</strong></section></main>
}
