import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Checkbox } from '@base-ui/react/checkbox'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getAgent, queryKeys } from '../../api/client'
import { entriesQueryOptions } from '../../api/queries'
import { Banner, gapLabel } from '../../shared/presentation'
import { transcriptNotice } from '../../shared/loadingPresentation'
import { agentVitalsPresentation } from '../../shared/agentVitals'
import { Composer } from '../composer/Composer'
import { persistCleanView, persistShowSystem, readCleanView, readShowSystem } from './cleanView'
import { QueuedMessages } from './QueuedMessages'
import { visibleQueuedMessages } from './queuedMessages'
import { TranscriptEntries } from './TranscriptEntries'
import { ScreenViewport } from '../screen/ScreenPanel'
import { agentScreenChoice } from '../screen/screenPresentation'

export function AgentPanel({ name, liveStatus, screenPaneID, onScreenPane, onViewer, identityReadOnly, onSend, onStatus }: { name: string, liveStatus: string, screenPaneID?: string, onScreenPane: (paneID?: string) => void, onViewer: (viewer: string) => void, identityReadOnly: string, onSend: () => void, onStatus: (name: string, status: string) => void }) {
  const queryClient = useQueryClient()
  const agentQuery = useQuery({ queryKey: queryKeys.agent(name), queryFn: () => getAgent(name), staleTime: 30_000, retry: false })
  const entriesQuery = useQuery(entriesQueryOptions(queryClient, name))
  const [showSystem, setShowSystem] = useState(() => readShowSystem(name))
  const [cleanView, setCleanView] = useState(() => readCleanView(name))
  const [now, setNow] = useState(Date.now())
  const [following, setFollowing] = useState(true)
  const [newEntryCount, setNewEntryCount] = useState(0)
  const [sendProblem, setSendProblem] = useState('')
  const transcriptRef = useRef<HTMLElement>(null)
  const followingRef = useRef(true)
  const previousEntryCount = useRef(0)
  const entries = entriesQuery.data?.entries ?? []
  const queued = visibleQueuedMessages(agentQuery.data?.queued ?? [], entries)
  const entriesNotice = transcriptNotice(entriesQuery.isPending, entriesQuery.error?.message ?? '')
  const screenChoice = agentScreenChoice(agentQuery.data, screenPaneID)
  const screenMode = screenChoice.active

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

  useLayoutEffect(() => {
    const added = Math.max(0, entries.length - previousEntryCount.current)
    previousEntryCount.current = entries.length
    if (!followingRef.current) {
      if (added) setNewEntryCount((count) => count + added)
      return
    }
    const transcript = transcriptRef.current
    if (transcript) transcript.scrollTop = transcript.scrollHeight
    setFollowing(true)
    setNewEntryCount(0)
  }, [entries, cleanView])

  if (agentQuery.error && 'response' in agentQuery.error && (agentQuery.error.response as Response)?.status === 404) return <main className="agent-page">
    <section className="not-found tombstone" role="status"><strong>No retained agent evidence</strong><p>No live or retained session evidence for <span>{name}</span>. This tab is safe to close.</p></section>
  </main>

  const agent = agentQuery.data
  const retired = agent?.bus_status === 'retired'
  const vitals = agent ? agentVitalsPresentation(agent) : []
  return <main className="agent-page">
    <header className="agent-header">
      <strong className="agent-name">{name}</strong>
      {agent && <><span className="pane-chip">{retired ? 'retired' : agent.pane?.pane_id ?? 'unplaced'}</span><span className="agent-status">{retired ? 'retired · read-only' : `${agent.herdr_status} · ${liveStatus !== '-' ? liveStatus : agent.bus_status}`}</span>{!retired && agent.gap !== '-' && <span className="gap-badge">{gapLabel(agent.gap)}</span>}<span className="tool-chip">{agent.tool}</span>{vitals.length > 0 && <span className="agent-vitals">{vitals.map((vital, index) => <span key={`${index}:${vital}`}>{vital}</span>)}</span>}</>}
      <div className="agent-actions">
        <div className="detail-toggle agent-view-toggle" aria-label="Agent view">
          <button type="button" className={screenMode ? '' : 'active'} aria-pressed={!screenMode} onClick={() => onScreenPane(undefined)}>Transcript</button>
          <button type="button" className={screenMode ? 'active' : ''} aria-pressed={screenMode} disabled={!screenChoice.enabled} title={screenChoice.reason || 'Show the read-only live screen'} onClick={() => { if (screenChoice.paneID) onScreenPane(screenChoice.paneID) }}>Screen</button>
        </div>
        {screenChoice.reason && <span className="view-reason">{screenChoice.reason}</span>}
        <label className="system-toggle"><Checkbox.Root checked={cleanView} onCheckedChange={(checked) => { setCleanView(checked); persistCleanView(name, checked) }}><Checkbox.Indicator>✓</Checkbox.Indicator></Checkbox.Root> clean view</label>
        <label className={`system-toggle${cleanView ? ' disabled' : ''}`} title={cleanView ? 'Clean view hides system entries' : undefined}><Checkbox.Root checked={showSystem} disabled={cleanView} onCheckedChange={(checked) => { setShowSystem(checked); persistShowSystem(name, checked) }}><Checkbox.Indicator>✓</Checkbox.Indicator></Checkbox.Root> show system entries</label>
        {!screenMode && <span className={`follow-chip${following ? '' : ' paused'}`}>{following ? 'follow ✓' : 'follow paused'}</span>}
      </div>
    </header>
    {agentQuery.error && <Banner source="agent" detail={agentQuery.error.message} />}
    {entriesNotice && <Banner source="transcript" detail={entriesNotice.detail} tone={entriesNotice.tone} />}
    {sendProblem && <Banner source="send" detail={sendProblem} />}
    {screenMode && screenPaneID ? <ScreenViewport paneID={screenPaneID} /> : <section className="transcript" aria-label="Transcript" ref={transcriptRef} onScroll={(event) => {
      const node = event.currentTarget
      const atBottom = node.scrollHeight - node.scrollTop - node.clientHeight < 48
      followingRef.current = atBottom
      setFollowing(atBottom)
      if (atBottom) setNewEntryCount(0)
    }}>
      <div className="window-note">Showing the latest {entries.length} classified entries · live from byte {entriesQuery.data?.nextOffset ?? '…'}</div>
      {entries.length === 0 && agent && <p className="empty">No renderable entries in this window.</p>}
      <TranscriptEntries entries={entries} agentName={name} now={now} showSystem={cleanView ? false : showSystem} cleanView={cleanView} />
      {newEntryCount > 0 && <button className="jump-latest" onClick={() => {
        const transcript = transcriptRef.current
        if (transcript) transcript.scrollTop = transcript.scrollHeight
        followingRef.current = true
        setFollowing(true)
        setNewEntryCount(0)
      }}>↓ {newEntryCount} new</button>}
    </section>}
    {!retired && <div className="queued-dock"><QueuedMessages messages={queued} now={now} /></div>}
    {agent && <Composer name={name} onViewer={onViewer} identityReadOnly={retired ? 'This agent is retired. Its retained transcript is read-only.' : identityReadOnly} onProblem={setSendProblem} onSend={onSend} />}
  </main>
}
