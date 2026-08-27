import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Checkbox } from '@base-ui/react/checkbox'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getAgent, queryKeys } from '../../api/client'
import { entriesQueryOptions } from '../../api/queries'
import { Banner, gapLabel } from '../../shared/presentation'
import { Composer } from '../composer/Composer'
import { TranscriptEntries } from './TranscriptEntries'

export function AgentPanel({ name, liveStatus, onViewer, identityReadOnly }: { name: string, liveStatus: string, onViewer: (viewer: string) => void, identityReadOnly: string }) {
  const queryClient = useQueryClient()
  const agentQuery = useQuery({ queryKey: queryKeys.agent(name), queryFn: () => getAgent(name), staleTime: 30_000, retry: false })
  const entriesQuery = useQuery(entriesQueryOptions(queryClient, name))
  const [showSystem, setShowSystem] = useState(false)
  const [now, setNow] = useState(Date.now())
  const [following, setFollowing] = useState(true)
  const [newEntryCount, setNewEntryCount] = useState(0)
  const [sendProblem, setSendProblem] = useState('')
  const transcriptRef = useRef<HTMLElement>(null)
  const followingRef = useRef(true)
  const previousEntryCount = useRef(0)
  const entries = entriesQuery.data?.entries ?? []

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
  }, [entries])

  if (agentQuery.error && 'response' in agentQuery.error && (agentQuery.error.response as Response)?.status === 404) return <main className="agent-page">
    <section className="not-found" role="alert"><strong>404 · Agent not found</strong><p>{agentQuery.error.message}</p></section>
  </main>

  const agent = agentQuery.data
  return <main className="agent-page">
    <header className="agent-header">
      <strong className="agent-name">{name}</strong>
      {agent && <><span className="pane-chip">{agent.pane?.pane_id ?? 'unplaced'}</span><span className="agent-status">{agent.herdr_status} · {liveStatus !== '-' ? liveStatus : agent.bus_status}</span>{agent.gap !== '-' && <span className="gap-badge">{gapLabel(agent.gap)}</span>}<span className="tool-chip">{agent.tool}</span></>}
      <div className="agent-actions"><label className="system-toggle"><Checkbox.Root checked={showSystem} onCheckedChange={setShowSystem}><Checkbox.Indicator>✓</Checkbox.Indicator></Checkbox.Root> show system entries</label><span className={`follow-chip${following ? '' : ' paused'}`}>{following ? 'follow ✓' : 'follow paused'}</span></div>
    </header>
    {agentQuery.error && <Banner source="agent" detail={agentQuery.error.message} />}
    {entriesQuery.error && <Banner source="transcript" detail={entriesQuery.error.message} />}
    {sendProblem && <Banner source="send" detail={sendProblem} />}
    <section className="transcript" aria-label="Transcript" ref={transcriptRef} onScroll={(event) => {
      const node = event.currentTarget
      const atBottom = node.scrollHeight - node.scrollTop - node.clientHeight < 48
      followingRef.current = atBottom
      setFollowing(atBottom)
      if (atBottom) setNewEntryCount(0)
    }}>
      <div className="window-note">Showing the latest {entries.length} classified entries · live from byte {entriesQuery.data?.nextOffset ?? '…'}</div>
      {entries.length === 0 && agent && <p className="empty">No renderable entries in this window.</p>}
      <TranscriptEntries entries={entries} agentName={name} now={now} showSystem={showSystem} />
      {newEntryCount > 0 && <button className="jump-latest" onClick={() => {
        const transcript = transcriptRef.current
        if (transcript) transcript.scrollTop = transcript.scrollHeight
        followingRef.current = true
        setFollowing(true)
        setNewEntryCount(0)
      }}>↓ {newEntryCount} new</button>}
    </section>
    {agent && <Composer name={name} onViewer={onViewer} identityReadOnly={identityReadOnly} onProblem={setSendProblem} />}
  </main>
}
