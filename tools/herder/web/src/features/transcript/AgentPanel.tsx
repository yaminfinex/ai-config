import { useCallback, useEffect, useState, type MouseEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getAgent, queryKeys } from '../../api/client'
import { entriesQueryOptions } from '../../api/queries'
import { Banner, ToolBadge } from '../../shared/presentation'
import { transcriptNotice } from '../../shared/loadingPresentation'
import { ScrollJumpButtons, useFollowScroll } from '../../shared/useFollowScroll'
import { Composer } from '../composer/Composer'
import { persistTranscriptViewMode, readTranscriptViewMode } from './cleanView'
import { AgentContextStrip } from './AgentContextStrip'
import { QueuedMessages } from './QueuedMessages'
import { visibleQueuedMessages } from './queuedMessages'
import { TranscriptEntries } from './TranscriptEntries'
import { transcriptUnavailable } from './transcriptUnavailable'
import { ScreenViewport } from '../screen/ScreenPanel'
import { agentScreenChoice } from '../screen/screenPresentation'
import { useTranscriptFileResolver } from '../files/TranscriptFileResolver'
import type { FileTarget, FolderTarget } from '../../types'
import type { AgentMentionMatcher } from '../../shared/agentMentions'
import { openInSideLabel, placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { PanelState } from '../../shared/PanelState'
import { AgentNotesStrip } from '../notes/AgentNotesStrip'
import { useNoteCapture } from '../notes/useNoteCapture'
import { useNotes } from '../notes/NotesProvider'
import { queueComposerNote } from '../notes/noteQueue'

export function AgentPanel({ name, agents, active, liveStatus, screenPaneID, mentionMatcher, onOpenAgent, onScreenPane, onOpenFile, onOpenFolder, onOpenChanges, onViewer, identityReadOnly, onSend, onStatus, onTerminalFocus }: { name: string, agents: string[], active: boolean, liveStatus: string, screenPaneID?: string, mentionMatcher: AgentMentionMatcher, onOpenAgent: (name: string, placement?: OpenPlacement) => void, onScreenPane: (paneID?: string) => void, onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void, onOpenFolder: (target: FolderTarget, placement?: OpenPlacement) => void, onOpenChanges: (root: string, placement?: OpenPlacement) => void, onViewer: (viewer: string) => void, identityReadOnly: string, onSend: () => void, onStatus: (name: string, status: string) => void, onTerminalFocus: (paneID?: string) => void }) {
  const queryClient = useQueryClient()
  const agentQuery = useQuery({ queryKey: queryKeys.agent(name), queryFn: () => getAgent(name), staleTime: 30_000, retry: false })
  const entriesQuery = useQuery(entriesQueryOptions(queryClient, name))
  const [viewMode, setViewMode] = useState(() => readTranscriptViewMode(name))
  const [now, setNow] = useState(Date.now())
  const [sendProblem, setSendProblem] = useState('')
  const agent = agentQuery.data
  const entries = entriesQuery.data?.entries ?? []
  const queued = visibleQueuedMessages(agent?.queued ?? [], entries)
  const unavailable = transcriptUnavailable(entriesQuery.error, agent?.parent_agent)
  const unavailableParent = unavailable?.parent
  const entriesNotice = transcriptNotice(entriesQuery.isPending, unavailable ? '' : entriesQuery.error?.message ?? '')
  const screenChoice = agentScreenChoice(agentQuery.data, screenPaneID)
  const screenMode = screenChoice.active
  const transcriptFollow = useFollowScroll<HTMLElement>(entries, viewMode, active && !screenMode)
  const cleanView = viewMode === 'compact'
  const showSystem = viewMode === 'full'
  const sideHint = openInSideLabel(navigator.userAgent)
  const openMention = useCallback((agentName: string, event: MouseEvent<HTMLElement>) => onOpenAgent(agentName, placementFromModifiers(event.nativeEvent)), [onOpenAgent])
  const selectViewMode = (mode: typeof viewMode) => {
    setViewMode(mode)
    persistTranscriptViewMode(name, mode)
  }
  const fileResolver = useTranscriptFileResolver(name, active && !screenMode, onOpenFile, onOpenFolder)
  const noteCapture = useNoteCapture({ active: active && !screenMode, source: { kind: 'transcript', agent: name }, agents })
  const { store: notesStore, announce: announceNote } = useNotes()

  useEffect(() => {
    if (agentQuery.data) onStatus(name, agentQuery.data.bus_status)
    else if (agentQuery.error && 'response' in agentQuery.error && (agentQuery.error.response as Response)?.status === 404) onStatus(name, 'unknown')
  }, [agentQuery.data, agentQuery.error, name, onStatus])

  useEffect(() => {
    if (!screenPaneID || !agentQuery.data) return
    const currentPaneID = agentScreenChoice(agentQuery.data, screenPaneID).paneID
    if (!currentPaneID) onScreenPane(undefined)
    else if (currentPaneID !== screenPaneID) onScreenPane(currentPaneID)
  }, [agentQuery.data, onScreenPane, screenPaneID])

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(timer)
  }, [])

  if (agentQuery.error && 'response' in agentQuery.error && (agentQuery.error.response as Response)?.status === 404) return <main className="agent-page">
    <PanelState className="not-found tombstone" title="No retained agent evidence" detail={<>No live or retained session evidence for <span>{name}</span>. This tab is safe to close.</>} />
  </main>

  const retired = agent?.bus_status === 'retired'
  return <main className="agent-page" ref={noteCapture.containerRef} onDoubleClickCapture={noteCapture.onDoubleClick}>
    <header className="agent-header">
      <strong className="agent-name">{name}</strong>
      <ToolBadge tool={agent?.tool} />
      <div className="agent-actions">
        <div className="detail-toggle agent-view-toggle" aria-label="Agent view">
          <button type="button" className={screenMode ? '' : 'active'} aria-pressed={!screenMode} onClick={() => onScreenPane(undefined)}>Transcript</button>
          <button type="button" className={screenMode ? 'active' : ''} aria-pressed={screenMode} disabled={!screenChoice.enabled} title={screenChoice.reason || 'Show the live terminal'} onClick={() => { if (screenChoice.paneID) onScreenPane(screenChoice.paneID) }}>Screen</button>
        </div>
        {screenChoice.reason && <span className="view-reason">{screenChoice.reason}</span>}
        <div className="detail-toggle transcript-mode-toggle" aria-label="Transcript detail">
          <button type="button" className={viewMode === 'compact' ? 'active' : ''} aria-pressed={viewMode === 'compact'} title="Compact conversation with activity pills" onClick={() => selectViewMode('compact')}>Compact</button>
          <button type="button" className={viewMode === 'normal' ? 'active' : ''} aria-pressed={viewMode === 'normal'} title="Full transcript without system entries" onClick={() => selectViewMode('normal')}>Normal</button>
          <button type="button" className={viewMode === 'full' ? 'active' : ''} aria-pressed={viewMode === 'full'} title="Full transcript including system entries" onClick={() => selectViewMode('full')}>Full</button>
        </div>
      </div>
    </header>
    {agentQuery.error && <Banner source="agent" detail={agentQuery.error.message} />}
    {entriesNotice && <Banner source="transcript" detail={entriesNotice.detail} tone={entriesNotice.tone} />}
    {sendProblem && <Banner source="send" detail={sendProblem} />}
    {screenMode && screenPaneID ? <ScreenViewport paneID={screenPaneID} active={active} onFocus={() => onTerminalFocus(screenPaneID)} onBlur={() => onTerminalFocus(undefined)} /> : <div className="transcript-viewport">
      <section className="transcript" data-follow-scroll aria-label="Transcript" ref={transcriptFollow.viewportRef} onScroll={transcriptFollow.onScroll} onDoubleClick={fileResolver.onDoubleClick}>
        {unavailable ? <PanelState className="transcript-unavailable" title={unavailable.title} detail={unavailable.detail}>
          {unavailableParent && <button type="button" onClick={() => onOpenAgent(unavailableParent)}>Open parent</button>}
        </PanelState> : <>
          <div className="window-note">Showing the latest {entries.length} classified entries · live from byte {entriesQuery.data?.nextOffset ?? '…'}</div>
          {entries.length === 0 && agent && <p className="empty">No renderable entries in this window.</p>}
          <TranscriptEntries entries={entries} agentName={name} now={now} showSystem={showSystem} cleanView={cleanView} mentionMatcher={mentionMatcher} onOpenAgent={openMention} sideHint={sideHint} />
        </>}
      </section>
      {fileResolver.element}
      <ScrollJumpButtons bottomVisible={!transcriptFollow.following} onBottom={transcriptFollow.jumpToBottom} />
    </div>}
    {!retired && <div className="queued-dock"><QueuedMessages messages={queued} now={now} /></div>}
    <AgentContextStrip agent={agent} liveStatus={liveStatus} onOpenFolder={onOpenFolder} onOpenChanges={onOpenChanges} />
    {agent && <AgentNotesStrip agent={name} agents={agents} />}
    {agent && <Composer name={name} onViewer={onViewer} identityReadOnly={retired ? 'This agent is retired. Its retained transcript is read-only.' : identityReadOnly} onProblem={setSendProblem} onSend={onSend} onQueue={(text) => {
      const result = queueComposerNote(notesStore, name, text)
      if (!result.ok) return result
      announceNote(`Queued a note for ${name}.`)
      return { ok: true }
    }} />}
    {noteCapture.element}
  </main>
}
