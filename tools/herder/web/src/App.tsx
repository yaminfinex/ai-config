import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import type {
  AgentDetail,
  Board,
  LifecycleResult,
  Pane,
  Refusal,
  Row,
  SubstrateEvent,
  TranscriptExchange,
  TranscriptPage,
} from './types'

const layoutKey = 'herder.web.layout.v1'
const boardTab = { id: 'board', kind: 'board' as const, label: 'Board' }
const defaultSidebarWidth = 250

type ShellTab = typeof boardTab | { id: string, kind: 'agent', label: string, name: string }
type StoredLayout = { openTabs: string[], activeTab: string, sidebarWidth: number }

function agentTab(name: string): ShellTab {
  return { id: `agent:${name}`, kind: 'agent', label: name, name }
}

function clampSidebarWidth(width: number) {
  return Math.min(440, Math.max(200, width))
}

function readLayout(): { tabs: ShellTab[], activeTab: string, sidebarWidth: number } {
  try {
    const stored = JSON.parse(localStorage.getItem(layoutKey) ?? '') as Partial<StoredLayout>
    if (!Array.isArray(stored.openTabs) || stored.openTabs.some((name) => typeof name !== 'string' || !name) ||
      typeof stored.activeTab !== 'string' || typeof stored.sidebarWidth !== 'number' || !Number.isFinite(stored.sidebarWidth)) throw new Error('invalid layout')
    const names = [...new Set(stored.openTabs)]
    const tabs = [boardTab, ...names.map(agentTab)]
    if (!tabs.some((tab) => tab.id === stored.activeTab)) throw new Error('invalid active tab')
    const activeTab = stored.activeTab
    const sidebarWidth = clampSidebarWidth(stored.sidebarWidth)
    return { tabs, activeTab, sidebarWidth }
  } catch {
    return { tabs: [boardTab], activeTab: boardTab.id, sidebarWidth: defaultSidebarWidth }
  }
}

type LifecycleProblem = {
  inline?: string
  readOnly?: string
  banner?: string
}

function without(problem: Record<string, string>, key: string) {
  const next = { ...problem }
  delete next[key]
  return next
}

async function refusal(response: Response): Promise<Refusal> {
  try {
    const body = await response.json() as Partial<Refusal>
    return {
      error: body.error ?? `HTTP ${response.status}`,
      detail: body.detail ?? response.statusText,
    }
  } catch {
    return { error: `HTTP ${response.status}`, detail: response.statusText }
  }
}

function navigate(path: string) {
  window.history.pushState({}, '', path)
  window.dispatchEvent(new PopStateEvent('popstate'))
}

function AppLink({ to, className, children }: { to: string, className?: string, children: React.ReactNode }) {
  return <a href={to} className={className} onClick={(event) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()
    navigate(to)
  }}>{children}</a>
}

async function lifecycleProblem(response: Response): Promise<LifecycleProblem> {
  const problem = await refusal(response)
  if (response.status === 409 && problem.error === 'attribution required') {
    return { readOnly: `Connect via Tailscale to continue. ${problem.detail}` }
  }
  if (response.status === 502) return { banner: problem.detail }
  if (response.status === 409) return { inline: problem.detail }
  return { inline: `${problem.error}: ${problem.detail}` }
}

function placementNotice(action: string, result: LifecycleResult) {
  return `${action} ${result.name} · ${result.pane || 'placement pending'}`
}

function SpawnControl({ pane, onBanner }: { pane: Pane, onBanner: (key: string, detail: string) => void }) {
  const [open, setOpen] = useState(false)
  const [shape, setShape] = useState<'pane' | 'tab' | 'worktree'>('pane')
  const [tool, setTool] = useState<'claude' | 'codex'>('codex')
  const [tag, setTag] = useState('')
  const [prompt, setPrompt] = useState('')
  const [branch, setBranch] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [inlineProblem, setInlineProblem] = useState('')
  const [readOnly, setReadOnly] = useState('')
  const [notice, setNotice] = useState('')
  const problemKey = `spawn ${pane.pane_id}`

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (submitting || readOnly) return
    setSubmitting(true)
    setInlineProblem('')
    setNotice('')
    onBanner(problemKey, '')
    try {
      const body: Record<string, string> = {
        from_pane: pane.pane_id,
        shape,
        tool,
        tag,
        prompt,
      }
      if (shape === 'worktree') body.branch = branch
      const response = await fetch('/api/spawn', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!response.ok) {
        const problem = await lifecycleProblem(response)
        if (problem.readOnly) setReadOnly(problem.readOnly)
        if (problem.inline) setInlineProblem(problem.inline)
        if (problem.banner) onBanner(problemKey, problem.banner)
        return
      }
      const result = await response.json() as LifecycleResult
      setNotice(placementNotice('Started', result))
    } catch (error: unknown) {
      onBanner(problemKey, error instanceof Error ? error.message : String(error))
    } finally {
      setSubmitting(false)
    }
  }

  if (!open) return <button type="button" className="compact" onClick={() => setOpen(true)}>Spawn</button>
  return (
    <form className="lifecycle-form spawn-form" onSubmit={(event) => void submit(event)}>
      <div className="lifecycle-heading"><strong>Spawn from {pane.pane_id}</strong><button type="button" className="compact" disabled={submitting} onClick={() => setOpen(false)}>Close</button></div>
      {readOnly && <div className="read-only" role="alert"><strong>Read-only</strong><span>{readOnly}</span></div>}
      <div className="lifecycle-fields">
        <label>Shape<select value={shape} disabled={submitting || Boolean(readOnly)} onChange={(event) => setShape(event.target.value as typeof shape)}><option value="pane">Same tab</option><option value="tab">Same workspace</option><option value="worktree">New worktree</option></select></label>
        <label>Tool<select value={tool} disabled={submitting || Boolean(readOnly)} onChange={(event) => setTool(event.target.value as typeof tool)}><option value="claude">Claude</option><option value="codex">Codex</option></select></label>
        <label>Tag<input value={tag} required disabled={submitting || Boolean(readOnly)} onChange={(event) => setTag(event.target.value)} /></label>
        {shape === 'worktree' && <label>Branch<input value={branch} required disabled={submitting || Boolean(readOnly)} onChange={(event) => setBranch(event.target.value)} /></label>}
      </div>
      <label>Prompt<textarea rows={3} value={prompt} required disabled={submitting || Boolean(readOnly)} onChange={(event) => setPrompt(event.target.value)} /></label>
      <div className="send-footer">
        <div>{inlineProblem && <p className="inline-error" role="alert">{inlineProblem}</p>}{notice && <p className="send-notice">{notice}</p>}</div>
        <button type="submit" disabled={submitting || Boolean(readOnly)}>{submitting ? 'Spawning… this can take up to 150s' : 'Spawn agent'}</button>
      </div>
    </form>
  )
}

function gapLabel(gap: string) {
  if (gap === '-') return ''
  return gap.toLowerCase().includes('pane') ? 'no pane' : 'gap'
}

function RowCells({ row, spawning = false, onBanner = () => {} }: { row: Row | Pane, spawning?: boolean, onBanner?: (key: string, detail: string) => void }) {
  const hasAgent = row.agent !== '-'
  return (
    <>
      <td className="pane-id">{row.pane_id}</td>
      <td className="agent-cell">{hasAgent
        ? <><AppLink to={`/agents/${encodeURIComponent(row.agent)}`}>{row.agent}</AppLink><span>{row.tool}</span></>
        : <><span>{'label' in row && row.label ? row.label : 'shell'}</span><span>shell</span></>}</td>
      <td className="status-cell"><span className={`status-dot ${statusClass(row.herdr_status)}`} />{row.herdr_status} · {row.bus_status !== '-' ? row.bus_status : 'no bus'}{hasAgent && gapLabel(row.gap) && <span className="gap-badge">{gapLabel(row.gap)}</span>}</td>
      <td className="actions-cell">{spawning && hasAgent && <SpawnControl pane={row as Pane} onBanner={onBanner} />}</td>
    </>
  )
}

function Rows({ rows, spawning = false, onBanner = () => {} }: { rows: Array<Row | Pane>, spawning?: boolean, onBanner?: (key: string, detail: string) => void }) {
  return <div className="table-wrap"><table><thead><tr><th>Pane</th><th>Agent</th><th>Status</th><th>Actions</th></tr></thead><tbody>{rows.map((row) => (
    <tr key={`${row.pane_id}:${row.agent}`} className={row.agent !== '-' ? 'agent-table-row' : undefined} onClick={(event) => {
      if (row.agent !== '-' && !(event.target as HTMLElement).closest('button, a, form')) navigate(`/agents/${encodeURIComponent(row.agent)}`)
    }}><RowCells row={row} spawning={spawning} onBanner={onBanner} /></tr>
  ))}</tbody></table></div>
}

function useFleet(agentNames: string[]) {
  const [board, setBoard] = useState<Board | null>(null)
  const [problems, setProblems] = useState<Record<string, string>>({ stream: 'Connecting to live fleet…' })
  const [messages, setMessages] = useState(0)
  const [lastEvent, setLastEvent] = useState<Date | null>(null)
  const [streamGeneration, setStreamGeneration] = useState(0)
  const [transcriptEvents, setTranscriptEvents] = useState<Record<string, TranscriptExchange[]>>({})
  const [transcriptResets, setTranscriptResets] = useState<Record<string, number>>({})
  const subscription = [...new Set(agentNames)].sort().join(',')
  const setLifecycleBanner = (key: string, detail: string) => setProblems((current) => detail
    ? { ...current, [key]: detail }
    : without(current, key))

  useEffect(() => {
    let active = true
    const firstPaint = async () => {
      try {
        const response = await fetch('/api/fleet')
        if (!response.ok) throw new Error((await refusal(response)).detail)
        const snapshot = await response.json() as Board
        if (active) setBoard(snapshot)
      } catch (error: unknown) {
        if (active) setProblems((current) => ({ ...current, fleet: error instanceof Error ? error.message : String(error) }))
      }
    }
    void firstPaint()
    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    let active = true
    let events: EventSource | null = null
    let reconnectTimer: number | null = null
    let watchdog: number | null = null
    let backoff = 500
    let lastActivity = Date.now()
    const names = subscription ? subscription.split(',') : []
    const subscribed = new Set(names)
    setTranscriptEvents((current) => Object.fromEntries(Object.entries(current).filter(([name]) => subscribed.has(name))))
    setTranscriptResets((current) => Object.fromEntries(Object.entries(current).filter(([name]) => subscribed.has(name))))
    const touch = (visible = true) => {
      lastActivity = Date.now()
      if (visible) setLastEvent(new Date())
    }
    const scheduleReconnect = (detail: string) => {
      if (!active || reconnectTimer !== null) return
      events?.close()
      events = null
      setProblems((current) => ({ ...current, stream: detail }))
      reconnectTimer = window.setTimeout(() => {
        reconnectTimer = null
        connect()
      }, backoff)
      backoff = Math.min(backoff * 2, 10_000)
    }
    const connect = () => {
      if (!active) return
      lastActivity = Date.now()
      const params = new URLSearchParams()
      if (subscription) params.set('agents', subscription)
      const url = `/api/events${params.size ? `?${params.toString()}` : ''}`
      setProblems((current) => ({ ...current, stream: 'Connecting to live fleet…' }))
      try {
        events = new EventSource(url)
      } catch {
        scheduleReconnect('Live stream disconnected; reconnecting…')
        return
      }
      events.onopen = () => {
        touch()
        backoff = 500
        setStreamGeneration((generation) => generation + 1)
        setProblems((current) => without(current, 'stream'))
      }
      events.onerror = () => scheduleReconnect('Live stream disconnected; reconnecting…')
      events.addEventListener('ping', () => touch(false))
      events.addEventListener('fleet', (event) => {
        touch()
        setBoard(JSON.parse(event.data) as Board)
        setProblems((current) => without(current, 'fleet'))
      })
      events.addEventListener('substrate', (event) => {
        touch()
        const state = JSON.parse(event.data) as SubstrateEvent
        setProblems((current) => {
          if (state.status === 'recovered') return without(current, state.source)
          return { ...current, [state.source]: state.detail ?? `${state.source} is unreachable` }
        })
      })
      events.addEventListener('message', () => {
        touch()
        setMessages((count) => count + 1)
      })
      events.addEventListener('rewindow', (event) => {
        touch()
        const reset = JSON.parse(event.data) as { agent: string }
        setTranscriptEvents((current) => {
          const next = { ...current }
          delete next[reset.agent]
          return next
        })
        setTranscriptResets((current) => ({ ...current, [reset.agent]: (current[reset.agent] ?? 0) + 1 }))
      })
      names.forEach((name) => events?.addEventListener(`exchange:${name}`, (event) => {
        touch()
        const incoming = JSON.parse(event.data) as TranscriptExchange
        setTranscriptEvents((current) => {
          const existing = current[name] ?? []
          if (existing.some((item) => item.position === incoming.position)) return current
          return { ...current, [name]: [...existing, incoming] }
        })
      }))
    }
    connect()
    watchdog = window.setInterval(() => {
      if (Date.now() - lastActivity > 45_000) scheduleReconnect('Live stream timed out; reconnecting…')
    }, 5_000)
    return () => {
      active = false
      events?.close()
      if (reconnectTimer !== null) window.clearTimeout(reconnectTimer)
      if (watchdog !== null) window.clearInterval(watchdog)
    }
  }, [subscription])

  return { board, problems, messages, lastEvent, setLifecycleBanner, streamGeneration, transcriptEvents, transcriptResets }
}

function workspaceName(label: string, id: string) {
  return (label || id).replace(/-[0-9a-f]{8}$/i, '')
}

function BoardPanel({ board, onBanner }: { board: Board | null, onBanner: (key: string, detail: string) => void }) {
  return (
    <main className="panel-scroll board-panel">
      <header className="board-header"><strong>Fleet board</strong><span>{board ? `${board.workspaces.length} workspaces · ${board.workspaces.reduce((count, workspace) => count + workspace.pane_count, 0)} panes` : 'connecting'}</span></header>
      {!board ? <p className="loading">Waiting for the first fleet snapshot…</p> : (
        <>
          <section className="fleet" aria-label="Workspaces">
            {board.workspaces.map((workspace) => (
              <article className="workspace" key={workspace.workspace_id}>
                <div className="section-heading">
                  <h2 title={workspace.label || workspace.workspace_id}>{workspaceName(workspace.label, workspace.workspace_id)} {workspace.focused && <span className="focused">focused</span>}</h2>
                  <span>{workspace.tab_count} tabs · {workspace.pane_count} panes · {workspace.agent_status || '—'}</span>
                </div>
                <div className="table-wrap"><table><thead><tr><th>Pane</th><th>Agent</th><th>Status</th><th>Actions</th></tr></thead><tbody>
                  {workspace.tabs.map((tab) => <Fragment key={tab.tab_id}>
                    <tr className="tab-separator"><th colSpan={4}>tab {tab.number}: {tab.label || tab.tab_id} {tab.focused && <span>focused</span>}</th></tr>
                    {tab.panes.map((row) => <tr key={`${row.pane_id}:${row.agent}`} className={row.agent !== '-' ? 'agent-table-row' : undefined} onClick={(event) => {
                      if (row.agent !== '-' && !(event.target as HTMLElement).closest('button, a, form')) navigate(`/agents/${encodeURIComponent(row.agent)}`)
                    }}><RowCells row={row} spawning onBanner={onBanner} /></tr>)}
                  </Fragment>)}
                </tbody></table></div>
              </article>
            ))}
          </section>
          <section className="workspace unplaced">
            <div className="section-heading"><h2>Unplaced</h2><span>{board.unplaced.length} agents</span></div>
            {board.unplaced.length ? <Rows rows={board.unplaced} /> : <p className="empty">No placement gaps.</p>}
          </section>
        </>
      )}
    </main>
  )
}

function Banner({ source, detail }: { source: string, detail: string }) {
  return <div className="banner" role="alert"><strong>{source}</strong><span>{detail}</span></div>
}

function valueText(value: unknown): string | null {
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return null
}

function Exchange({ exchange, detail }: { exchange: TranscriptExchange, detail: 'exchanges' | 'full' }) {
  const prompt = valueText(exchange.user ?? exchange.prompt ?? exchange.request)
  const reply = valueText(exchange.action ?? exchange.reply ?? exchange.response)
  const known = new Set(['position', 'user', 'prompt', 'request', 'action', 'reply', 'response'])
  const extra = Object.fromEntries(Object.entries(exchange).filter(([key]) => !known.has(key)))
  return (
    <article className="exchange">
      <div className="exchange-number">Exchange {exchange.position}</div>
      {prompt && <div className="turn prompt"><span>You</span><p>{prompt}</p></div>}
      {reply && <div className="turn reply"><span>Agent</span><p>{reply}</p></div>}
      {!prompt && !reply && <pre>{JSON.stringify(exchange, null, 2)}</pre>}
      {detail === 'full' && Object.keys(extra).length > 0 && <details><summary>Tool-level detail</summary><pre>{JSON.stringify(extra, null, 2)}</pre></details>}
    </article>
  )
}

function ForkControl({ name, onBanner }: { name: string, onBanner: (key: string, detail: string) => void }) {
  const [open, setOpen] = useState(false)
  const [prompt, setPrompt] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [inlineProblem, setInlineProblem] = useState('')
  const [readOnly, setReadOnly] = useState('')
  const [notice, setNotice] = useState('')
  const problemKey = 'fork'

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (submitting || readOnly) return
    setSubmitting(true)
    setInlineProblem('')
    setNotice('')
    onBanner(problemKey, '')
    try {
      const response = await fetch(`/api/agents/${encodeURIComponent(name)}/fork`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(prompt ? { prompt } : {}),
      })
      if (!response.ok) {
        const problem = await lifecycleProblem(response)
        if (problem.readOnly) setReadOnly(problem.readOnly)
        if (problem.inline) setInlineProblem(problem.inline)
        if (problem.banner) onBanner(problemKey, problem.banner)
        return
      }
      const result = await response.json() as LifecycleResult
      setNotice(placementNotice('Forked', result))
    } catch (error: unknown) {
      onBanner(problemKey, error instanceof Error ? error.message : String(error))
    } finally {
      setSubmitting(false)
    }
  }

  if (!open) return <button type="button" onClick={() => setOpen(true)}>Fork agent</button>
  return (
    <form className="lifecycle-form fork-form" onSubmit={(event) => void submit(event)}>
      <div className="lifecycle-heading"><strong>Fork {name}</strong><button type="button" className="compact" disabled={submitting} onClick={() => setOpen(false)}>Close</button></div>
      {readOnly && <div className="read-only" role="alert"><strong>Read-only</strong><span>{readOnly}</span></div>}
      <label>Opening prompt <span className="optional">optional</span><textarea rows={3} value={prompt} disabled={submitting || Boolean(readOnly)} onChange={(event) => setPrompt(event.target.value)} /></label>
      <div className="send-footer">
        <div>{inlineProblem && <p className="inline-error" role="alert">{inlineProblem}</p>}{notice && <p className="send-notice">{notice}</p>}</div>
        <button type="submit" disabled={submitting || Boolean(readOnly)}>{submitting ? 'Forking… this can take up to 150s' : 'Fork agent'}</button>
      </div>
    </form>
  )
}

function AgentPanel({ name, onViewer, streamed, streamGeneration, resetGeneration }: {
  name: string
  onViewer: (viewer: string) => void
  streamed: TranscriptExchange[]
  streamGeneration: number
  resetGeneration: number
}) {
  const [agent, setAgent] = useState<AgentDetail | null>(null)
  const [exchanges, setExchanges] = useState<TranscriptExchange[]>([])
  const [cursor, setCursor] = useState('')
  const [hasOlder, setHasOlder] = useState(true)
  const [detail, setDetail] = useState<'exchanges' | 'full'>('exchanges')
  const [problems, setProblems] = useState<Record<string, string>>({})
  const [notFound, setNotFound] = useState<Refusal | null>(null)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const [message, setMessage] = useState('')
  const [sendProblem, setSendProblem] = useState('')
  const [sendNotice, setSendNotice] = useState('')
  const [sending, setSending] = useState(false)
  const [readOnly, setReadOnly] = useState('')
  const currentView = useRef({ name, detail })
  currentView.current = { name, detail }
  const setLifecycleBanner = (key: string, problemDetail: string) => setProblems((current) => problemDetail
    ? { ...current, [key]: problemDetail }
    : without(current, key))

  useEffect(() => {
    let active = true
    setAgent(null)
    setExchanges([])
    setCursor('')
    setHasOlder(true)
    setLoadingOlder(false)
    setNotFound(null)
    setProblems({})

    const loadAgent = async () => {
      try {
        const agentResponse = await fetch(`/api/agents/${encodeURIComponent(name)}`)
        if (!agentResponse.ok) {
          const problem = await refusal(agentResponse)
          if (active && agentResponse.status === 404) setNotFound(problem)
          else throw new Error(problem.detail)
          return
        }
        const agentDetail = await agentResponse.json() as AgentDetail
        if (!active) return
        setAgent(agentDetail)
      } catch (error: unknown) {
        if (active) setProblems((current) => ({ ...current, transcript: error instanceof Error ? error.message : String(error) }))
      }
    }
    void loadAgent()
    return () => {
      active = false
    }
  }, [name, detail])

  useEffect(() => {
    if (streamed.length === 0) return
    setExchanges((current) => {
      const merged = new Map(current.map((item) => [item.position, item]))
      streamed.forEach((item) => merged.set(item.position, item))
      return [...merged.values()].sort((a, b) => a.position - b.position)
    })
  }, [streamed])

  const seenReset = useRef(resetGeneration)
  useEffect(() => {
    if (seenReset.current === resetGeneration) return
    seenReset.current = resetGeneration
    setExchanges([])
    setCursor('')
    setHasOlder(true)
  }, [resetGeneration])

  useEffect(() => {
    if (streamGeneration === 0) return
    let active = true
    const loadWindow = async () => {
      try {
        const response = await fetch(`/api/agents/${encodeURIComponent(name)}/transcript?limit=20&detail=${detail}`)
        if (!response.ok) throw new Error((await refusal(response)).detail)
        const page = await response.json() as TranscriptPage
        if (!active) return
        setExchanges((current) => {
          const merged = new Map(current.map((item) => [item.position, item]))
          page.exchanges.forEach((item) => merged.set(item.position, item))
          return [...merged.values()].sort((a, b) => a.position - b.position)
        })
        setCursor(page.cursor)
        setHasOlder(page.exchanges.length > 0)
        setProblems((current) => without(current, 'transcript'))
      } catch (error: unknown) {
        if (active) setProblems((current) => ({ ...current, transcript: error instanceof Error ? error.message : String(error) }))
      }
    }
    void loadWindow()
    return () => { active = false }
  }, [name, detail, streamGeneration, resetGeneration])

  const loadOlder = async () => {
    if (!cursor || loadingOlder) return
    const requestView = { name, detail }
    const requestIsCurrent = () => currentView.current.name === requestView.name && currentView.current.detail === requestView.detail
    setLoadingOlder(true)
    try {
      const response = await fetch(`/api/agents/${encodeURIComponent(name)}/transcript?limit=20&detail=${detail}&before=${encodeURIComponent(cursor)}`)
      if (!requestIsCurrent()) return
      if (!response.ok) throw new Error((await refusal(response)).detail)
      const page = await response.json() as TranscriptPage
      if (!requestIsCurrent()) return
      setCursor(page.cursor)
      setHasOlder(page.exchanges.length > 0)
      setExchanges((current) => {
        const positions = new Set(current.map((item) => item.position))
        return [...page.exchanges.filter((item) => !positions.has(item.position)), ...current]
      })
    } catch (error: unknown) {
      if (requestIsCurrent()) {
        setProblems((current) => ({ ...current, transcript: error instanceof Error ? error.message : String(error) }))
      }
    } finally {
      if (requestIsCurrent()) {
        setLoadingOlder(false)
      }
    }
  }

  const send = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!message.trim() || sending || readOnly) return
    setSending(true)
    setSendProblem('')
    setSendNotice('')
    try {
      const response = await fetch(`/api/agents/${encodeURIComponent(name)}/message`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: message }),
      })
      if (!response.ok) {
        const problem = await refusal(response)
        if (response.status === 409 && problem.error === 'attribution required') {
          setReadOnly(`Connect via Tailscale to send. ${problem.detail}`)
        } else if (response.status === 502) {
          setProblems((current) => ({ ...current, send: problem.detail }))
        } else {
          setSendProblem(`${problem.error}: ${problem.detail}`)
        }
        return
      }
      const result = await response.json() as { sent: boolean, to: string, from: string }
      onViewer(result.from)
      setMessage('')
      setSendNotice(`Sent to ${result.to} as ${result.from}. Waiting for the live reply…`)
      setProblems((current) => without(current, 'send'))
    } catch (error: unknown) {
      setProblems((current) => ({ ...current, send: error instanceof Error ? error.message : String(error) }))
    } finally {
      setSending(false)
    }
  }

  if (notFound) return (
    <main className="agent-page">
      <section className="not-found" role="alert"><strong>404 · Agent not found</strong><p>{notFound.detail}</p></section>
    </main>
  )

  return (
    <main className="agent-page">
      <header className="agent-header">
        <strong className="agent-name">{name}</strong>
        {agent && <><span className="pane-chip">{agent.pane?.pane_id ?? 'unplaced'}</span><span className="agent-status">{agent.herdr_status} · {agent.bus_status}</span>{agent.gap !== '-' && <span className="gap-badge">{gapLabel(agent.gap)}</span>}<span className="tool-chip">{agent.tool}</span></>}
        <div className="agent-actions"><div className="detail-toggle" aria-label="Transcript detail">
          <button className={detail === 'exchanges' ? 'active' : ''} onClick={() => setDetail('exchanges')}>Exchanges</button>
          <button className={detail === 'full' ? 'active' : ''} onClick={() => setDetail('full')}>Full</button>
        </div>{agent && <ForkControl name={name} onBanner={setLifecycleBanner} />}</div>
      </header>
      {Object.entries(problems).map(([source, problemDetail]) => <Banner source={source} detail={problemDetail} key={source} />)}
      <section className="transcript" aria-label="Transcript">
        <div className="older">
          {hasOlder
            ? <button disabled={!cursor || loadingOlder} onClick={() => void loadOlder()}>{loadingOlder ? 'Loading…' : 'Load older'}</button>
            : <span>Start of transcript</span>}
        </div>
        {exchanges.length === 0 && agent && <p className="empty">No exchanges in this window.</p>}
        {exchanges.map((exchange) => <Exchange exchange={exchange} detail={detail} key={exchange.position} />)}
      </section>
      {agent && <form className="send-box" onSubmit={(event) => void send(event)}>
        {readOnly && <div className="read-only" role="alert"><strong>Read-only</strong><span>{readOnly}</span></div>}
        <label htmlFor="message">Message {name}</label><textarea id="message" rows={2} value={message} disabled={Boolean(readOnly) || sending} onChange={(event) => setMessage(event.target.value)} placeholder="Send an attributed request…" />
        <div className="send-footer">
          <div>{sendProblem && <p className="inline-error" role="alert">{sendProblem}</p>}{sendNotice && <p className="send-notice">{sendNotice}</p>}</div>
          <button type="submit" disabled={!message.trim() || sending || Boolean(readOnly)}>{sending ? 'Sending…' : 'Send request'}</button>
        </div>
        <p className="attribution-copy">sends as an attributed web viewer · web senders are not addressable bus peers</p>
      </form>}
    </main>
  )
}

type Route = { page: 'board' } | { page: 'agent', name: string } | { page: 'missing' }

function currentRoute(): Route {
  const match = window.location.pathname.match(/^\/agents\/([^/]+)\/?$/)
  if (match) {
    try {
      return { page: 'agent', name: decodeURIComponent(match[1]) }
    } catch {
      return { page: 'missing' }
    }
  }
  return window.location.pathname === '/' ? { page: 'board' } : { page: 'missing' }
}

type TreeItem = { id: string, kind: 'workspace' | 'pane' | 'unplaced', parent?: string, agent?: string }

function statusClass(status: string) {
  if (status === 'working' || status === 'active') return 'working'
  if (status === 'idle' || status === 'listening') return 'idle'
  if (status === 'dead') return 'dead'
  return 'unknown'
}

function PaneTreeRow({ pane, tabLabel, focused, selected, setRef, onFocus, onOpen, onKeyDown }: {
  pane: Pane | Row
  tabLabel?: string
  focused: boolean
  selected: boolean
  setRef: (node: HTMLDivElement | null) => void
  onFocus: () => void
  onOpen: () => void
  onKeyDown: (event: React.KeyboardEvent) => void
}) {
  const hasAgent = pane.agent !== '-'
  const name = hasAgent ? pane.agent : ('label' in pane && pane.label) || pane.pane_id
  const signal = hasAgent && pane.bus_status !== '-' ? pane.bus_status : ''
  return <div
    ref={setRef}
    role="treeitem"
    aria-level={2}
    aria-selected={hasAgent && selected}
    tabIndex={focused ? 0 : -1}
    className={`tree-row pane-row${hasAgent ? ' agent-row' : ' shell-row'}${focused ? ' focused' : ''}`}
    title={`${pane.pane_id}${tabLabel ? ` · ${tabLabel}` : ''} · ${pane.tool} · herdr ${pane.herdr_status}${signal ? ` · bus ${signal}` : ''}`}
    onFocus={onFocus}
    onClick={onOpen}
    onKeyDown={onKeyDown}
  >
    <span className={`status-dot ${statusClass(pane.herdr_status)}`} aria-label={`Herdr ${pane.herdr_status}`} />
    <span className="tree-name">{name}</span>
    {signal && <span className="bus-status">{signal}</span>}
    {hasAgent && pane.gap !== '-' && <span className="gap-badge">{gapLabel(pane.gap)}</span>}
  </div>
}

function FleetSidebar({ board, activeAgent, onOpenAgent }: {
  board: Board | null
  activeAgent?: string
  onOpenAgent: (name: string) => void
}) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({ unplaced: true })
  const [focusID, setFocusID] = useState('')
  const refs = useRef(new Map<string, HTMLDivElement>())

  useEffect(() => {
    if (!board) return
    setExpanded((current) => {
      const next = { ...current }
      board.workspaces.forEach((workspace) => {
        if (!(workspace.workspace_id in next)) next[workspace.workspace_id] = true
      })
      return next
    })
    setFocusID((current) => current || board.workspaces[0]?.workspace_id || 'unplaced')
  }, [board])

  const items = useMemo(() => {
    const visible: TreeItem[] = []
    board?.workspaces.forEach((workspace) => {
      visible.push({ id: workspace.workspace_id, kind: 'workspace' })
      if (expanded[workspace.workspace_id]) {
        workspace.tabs.forEach((tab) => tab.panes.forEach((pane) => visible.push({
          id: `pane:${pane.pane_id}`,
          kind: 'pane',
          parent: workspace.workspace_id,
          agent: pane.agent !== '-' ? pane.agent : undefined,
        })))
      }
    })
    visible.push({ id: 'unplaced', kind: 'unplaced' })
    if (expanded.unplaced) board?.unplaced.forEach((row) => visible.push({ id: `unplaced:${row.agent}`, kind: 'pane', parent: 'unplaced', agent: row.agent }))
    return visible
  }, [board, expanded])

  const focus = (id: string) => {
    setFocusID(id)
    requestAnimationFrame(() => refs.current.get(id)?.focus())
  }
  const toggle = (id: string) => setExpanded((current) => ({ ...current, [id]: !current[id] }))
  const keyDown = (item: TreeItem) => (event: React.KeyboardEvent) => {
    const index = items.findIndex((candidate) => candidate.id === item.id)
    if (event.key === 'ArrowDown' && index < items.length - 1) focus(items[index + 1].id)
    else if (event.key === 'ArrowUp' && index > 0) focus(items[index - 1].id)
    else if (event.key === 'Home') focus(items[0].id)
    else if (event.key === 'End') focus(items[items.length - 1].id)
    else if (event.key === 'ArrowRight' && (item.kind === 'workspace' || item.kind === 'unplaced')) {
      if (!expanded[item.id]) toggle(item.id)
      else if (items[index + 1]?.parent === item.id) focus(items[index + 1].id)
    } else if (event.key === 'ArrowLeft') {
      if ((item.kind === 'workspace' || item.kind === 'unplaced') && expanded[item.id]) toggle(item.id)
      else if (item.parent) focus(item.parent)
    } else if (event.key === 'Enter' || event.key === ' ') {
      if (item.kind === 'workspace' || item.kind === 'unplaced') toggle(item.id)
      else if (item.agent) onOpenAgent(item.agent)
    } else return
    event.preventDefault()
  }

  const workspaceNode = (workspace: Board['workspaces'][number]) => {
    const item: TreeItem = { id: workspace.workspace_id, kind: 'workspace' }
    return <div className="workspace-tree" role="none" key={workspace.workspace_id}>
      <div
        ref={(node) => { if (node) refs.current.set(item.id, node); else refs.current.delete(item.id) }}
        role="treeitem"
        aria-level={1}
        aria-expanded={Boolean(expanded[item.id])}
        tabIndex={focusID === item.id ? 0 : -1}
        className={`tree-row workspace-row${focusID === item.id ? ' focused' : ''}`}
        onFocus={() => setFocusID(item.id)}
        onClick={() => toggle(item.id)}
        onKeyDown={keyDown(item)}
      >
        <span className="disclosure" aria-hidden="true">{expanded[item.id] ? '▾' : '▸'}</span>
        <span className="tree-name" title={workspace.label || workspace.workspace_id}>{workspaceName(workspace.label, workspace.workspace_id)}</span>
        <span className="count-badge">{workspace.pane_count}</span>
      </div>
      {expanded[item.id] && <div role="group">
        {workspace.tabs.flatMap((tab) => tab.panes.map((pane) => {
            const paneItem: TreeItem = { id: `pane:${pane.pane_id}`, kind: 'pane', parent: item.id, agent: pane.agent !== '-' ? pane.agent : undefined }
            return <PaneTreeRow
              key={pane.pane_id}
              pane={pane}
              tabLabel={`tab ${tab.number}: ${tab.label || tab.tab_id}`}
              focused={focusID === paneItem.id}
              selected={activeAgent === paneItem.agent}
              setRef={(node) => { if (node) refs.current.set(paneItem.id, node); else refs.current.delete(paneItem.id) }}
              onFocus={() => setFocusID(paneItem.id)}
              onOpen={() => { setFocusID(paneItem.id); if (paneItem.agent) onOpenAgent(paneItem.agent) }}
              onKeyDown={keyDown(paneItem)}
            />
          }))}
      </div>}
    </div>
  }

  const unplacedItem: TreeItem = { id: 'unplaced', kind: 'unplaced' }
  return <aside className="fleet-sidebar" aria-label="Fleet sidebar">
    <div className="sidebar-heading"><span className="status-dot working" /><strong>Fleet</strong><span>herdr truth</span></div>
    {!board ? <p className="sidebar-loading">Waiting for fleet…</p> : <div className="fleet-tree" role="tree" aria-label="Workspaces and panes">
      {board.workspaces.map(workspaceNode)}
      <div className="unplaced-tree" role="none">
        <div
          ref={(node) => { if (node) refs.current.set('unplaced', node); else refs.current.delete('unplaced') }}
          role="treeitem"
          aria-level={1}
          aria-expanded={Boolean(expanded.unplaced)}
          tabIndex={focusID === 'unplaced' ? 0 : -1}
          className={`tree-row workspace-row unplaced-row${focusID === 'unplaced' ? ' focused' : ''}`}
          onFocus={() => setFocusID('unplaced')}
          onClick={() => toggle('unplaced')}
          onKeyDown={keyDown(unplacedItem)}
        ><span className="disclosure" aria-hidden="true">{expanded.unplaced ? '▾' : '▸'}</span><span className="tree-name">Unplaced</span><span className="count-badge">{board.unplaced.length}</span></div>
        {expanded.unplaced && <div role="group">{board.unplaced.map((row) => {
          const item: TreeItem = { id: `unplaced:${row.agent}`, kind: 'pane', parent: 'unplaced', agent: row.agent }
          return <PaneTreeRow
            key={row.agent}
            pane={row}
            focused={focusID === item.id}
            selected={activeAgent === row.agent}
            setRef={(node) => { if (node) refs.current.set(item.id, node); else refs.current.delete(item.id) }}
            onFocus={() => setFocusID(item.id)}
            onOpen={() => { setFocusID(item.id); onOpenAgent(row.agent) }}
            onKeyDown={keyDown(item)}
          />
        })}</div>}
      </div>
    </div>}
  </aside>
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
  const [viewer, setViewer] = useState('unresolved')
  const tabRefs = useRef(new Map<string, HTMLButtonElement>())
  const agentNames = tabs.flatMap((tab) => tab.kind === 'agent' ? [tab.name] : [])
  const {
    board, problems, messages, lastEvent, setLifecycleBanner,
    streamGeneration, transcriptEvents, transcriptResets,
  } = useFleet(agentNames)
  const active = tabs.find((tab) => tab.id === activeTab) ?? boardTab

  const activate = (tab: ShellTab, push = true) => {
    setTabs((current) => current.some((item) => item.id === tab.id) ? current : [...current, tab])
    setActiveTab(tab.id)
    const path = tab.kind === 'board' ? '/' : `/agents/${encodeURIComponent(tab.name)}`
    if (push && window.location.pathname !== path) window.history.pushState({}, '', path)
  }

  useEffect(() => {
    const update = () => {
      const route = currentRoute()
      if (route.page === 'board') activate(boardTab, false)
      else if (route.page === 'agent') activate(agentTab(route.name), false)
    }
    window.addEventListener('popstate', update)
    return () => window.removeEventListener('popstate', update)
  })

  useEffect(() => {
    const value: StoredLayout = {
      openTabs: tabs.flatMap((tab) => tab.kind === 'agent' ? [tab.name] : []),
      activeTab,
      sidebarWidth,
    }
    try {
      localStorage.setItem(layoutKey, JSON.stringify(value))
    } catch {
      // Viewer persistence is best-effort; the live shell remains usable when
      // browser storage is unavailable or full.
    }
  }, [tabs, activeTab, sidebarWidth])

  const close = (id: string) => {
    const index = tabs.findIndex((tab) => tab.id === id)
    const nextTabs = tabs.filter((tab) => tab.id !== id)
    setTabs(nextTabs)
    if (activeTab === id) activate(nextTabs[Math.max(0, index - 1)] ?? boardTab)
  }
  const startResize = (event: React.PointerEvent) => {
    const startX = event.clientX
    const startWidth = sidebarWidth
    const move = (moveEvent: PointerEvent) => setSidebarWidth(clampSidebarWidth(startWidth + moveEvent.clientX - startX))
    const stop = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', stop)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop)
  }

  return <div className="app-shell">
    <div className="sidebar-region" style={{ width: sidebarWidth }}>
      <FleetSidebar board={board} activeAgent={active.kind === 'agent' ? active.name : undefined} onOpenAgent={(name) => activate(agentTab(name))} />
    </div>
    <div
      className="sidebar-resizer"
      role="separator"
      aria-label="Resize fleet sidebar"
      aria-orientation="vertical"
      aria-valuemin={200}
      aria-valuemax={440}
      aria-valuenow={sidebarWidth}
      tabIndex={0}
      onPointerDown={startResize}
      onKeyDown={(event) => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
        setSidebarWidth((width) => clampSidebarWidth(width + (event.key === 'ArrowRight' ? 10 : -10)))
        event.preventDefault()
      }}
    />
    <section className="shell-main">
      <div className="tab-strip" role="tablist" aria-label="Open panels">
        {tabs.map((tab, index) => <div role="presentation" className={`shell-tab${tab.id === activeTab ? ' active' : ''}`} key={tab.id}>
          <button
            ref={(node) => { if (node) tabRefs.current.set(tab.id, node); else tabRefs.current.delete(tab.id) }}
            id={`shell-tab-${index}`}
            aria-controls={`shell-panel-${index}`}
            role="tab"
            aria-selected={tab.id === activeTab}
            tabIndex={tab.id === activeTab ? 0 : -1}
            onClick={() => activate(tab)}
            onKeyDown={(event) => {
              let target = index
              if (event.key === 'ArrowLeft') target = (index - 1 + tabs.length) % tabs.length
              else if (event.key === 'ArrowRight') target = (index + 1) % tabs.length
              else if (event.key === 'Home') target = 0
              else if (event.key === 'End') target = tabs.length - 1
              else return
              activate(tabs[target])
              requestAnimationFrame(() => tabRefs.current.get(tabs[target].id)?.focus())
              event.preventDefault()
            }}
          >{tab.kind === 'board' ? '⌗ Board' : tab.label}</button>
          {tab.kind === 'agent' && <button className="close-tab" aria-label={`Close ${tab.label}`} onClick={() => close(tab.id)}>×</button>}
        </div>)}
        <button className="new-tab" type="button" title="Open agents from the fleet sidebar" aria-label="Open an agent from the fleet sidebar">+</button>
        <span className="tab-strip-spacer" />
        <span className={`stream-chip${problems.stream ? ' fault' : ''}`}>{problems.stream ? 'SSE: reconnecting' : 'SSE: connected'}</span>
        <span className="layout-chip">layout: this browser</span>
      </div>
      <div className="shell-banners">{Object.entries(problems).map(([source, detail]) => <Banner source={source} detail={detail} key={source} />)}</div>
      <div className="panel-host">
        {tabs.map((tab, index) => <div id={`shell-panel-${index}`} role="tabpanel" aria-labelledby={`shell-tab-${index}`} hidden={tab.id !== activeTab} className="hosted-panel" key={tab.id}>
          {tab.kind === 'board'
            ? <BoardPanel board={board} onBanner={setLifecycleBanner} />
            : <AgentPanel
              name={tab.name}
              onViewer={setViewer}
              streamed={transcriptEvents[tab.name] ?? []}
              streamGeneration={streamGeneration}
              resetGeneration={transcriptResets[tab.name] ?? 0}
            />}
        </div>)}
      </div>
      <footer className="status-bar">
        <span>substrate: herdr {board ? '✓' : '…'} · hcom {problems.hcom ? '×' : '✓'}</span>
        <span className={problems.stream ? 'fault' : ''}>SSE: {problems.stream ? 'reconnecting' : 'connected'}</span>
        <span>viewer: {viewer}</span><span>{viewer === 'unresolved' ? 'attribution resolves on send' : 'attributed'}</span>
        <span className="status-spacer" /><span>{messages} messages</span><span>last event: {lastEvent ? lastEvent.toLocaleTimeString() : '—'}</span>
      </footer>
    </section>
  </div>
}

export default function App() {
  const route = currentRoute()
  if (route.page !== 'missing') return <Shell initialRoute={route} />
  return <main className="agent-page"><AppLink to="/" className="back-link">← Fleet board</AppLink><section className="not-found"><strong>404 · Page not found</strong></section></main>
}
