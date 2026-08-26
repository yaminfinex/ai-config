import { useEffect, useMemo, useRef, useState } from 'react'
import type {
  AgentDetail,
  Board,
  LifecycleResult,
  Pane,
  Refusal,
  Row,
  SubstrateEvent,
  Tab,
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

const columns: Array<[keyof Row, string]> = [
  ['pane_id', 'Pane'],
  ['agent', 'Agent'],
  ['tool', 'Tool'],
  ['herdr_status', 'Herdr status'],
  ['bus_status', 'Bus status'],
  ['gap', 'Gap'],
]

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

function Rows({ rows, spawning = false, onBanner = () => {} }: { rows: Array<Row | Pane>, spawning?: boolean, onBanner?: (key: string, detail: string) => void }) {
  return (
    <div className="table-wrap">
      <table>
        <thead><tr>{columns.map(([key, label]) => <th key={key}>{label}</th>)}{spawning && <th>Actions</th>}</tr></thead>
        <tbody>
          {rows.map((row) => (
            <tr key={`${row.pane_id}:${row.agent}`} className={row.gap !== '-' ? 'has-gap' : undefined}>
              {columns.map(([key]) => <td key={key} data-label={key}>
                {key === 'agent' && row.agent !== '-'
                  ? <AppLink to={`/agents/${encodeURIComponent(row.agent)}`}>{row.agent}</AppLink>
                  : row[key]}
              </td>)}
              {spawning && <td>{row.agent !== '-' && <SpawnControl pane={row as Pane} onBanner={onBanner} />}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function useFleet() {
  const [board, setBoard] = useState<Board | null>(null)
  const [problems, setProblems] = useState<Record<string, string>>({ stream: 'Connecting to live fleet…' })
  const [messages, setMessages] = useState(0)
  const setLifecycleBanner = (key: string, detail: string) => setProblems((current) => detail
    ? { ...current, [key]: detail }
    : without(current, key))

  useEffect(() => {
    let active = true
    let events: EventSource | null = null
    const connect = () => {
      if (!active) return
      events = new EventSource('/api/events')
      events.onopen = () => setProblems((current) => without(current, 'stream'))
      events.onerror = () => setProblems((current) => ({ ...current, stream: 'Live stream disconnected; reconnecting…' }))
      events.addEventListener('fleet', (event) => {
        setBoard(JSON.parse(event.data) as Board)
        setProblems((current) => without(current, 'fleet'))
      })
      events.addEventListener('substrate', (event) => {
        const state = JSON.parse(event.data) as SubstrateEvent
        setProblems((current) => {
          if (state.status === 'recovered') return without(current, state.source)
          return { ...current, [state.source]: state.detail ?? `${state.source} is unreachable` }
        })
      })
      events.addEventListener('message', () => setMessages((count) => count + 1))
    }
    const firstPaint = async () => {
      try {
        const response = await fetch('/api/fleet')
        if (!response.ok) throw new Error((await refusal(response)).detail)
        const snapshot = await response.json() as Board
        if (active) setBoard(snapshot)
      } catch (error: unknown) {
        if (active) setProblems((current) => ({ ...current, fleet: error instanceof Error ? error.message : String(error) }))
      } finally {
        connect()
      }
    }
    void firstPaint()
    return () => {
      active = false
      events?.close()
    }
  }, [])

  return { board, problems, messages, setLifecycleBanner }
}

function BoardPanel({ board, messages, onBanner }: { board: Board | null, messages: number, onBanner: (key: string, detail: string) => void }) {
  return (
    <main className="panel-scroll board-panel">
      <header>
        <div><p className="eyebrow">Live substrate</p><h1>Herder fleet</h1></div>
        <div className="ticker" aria-label="Messages seen">{messages} messages seen</div>
      </header>
      {!board ? <p className="loading">Waiting for the first fleet snapshot…</p> : (
        <>
          <section className="fleet" aria-label="Workspaces">
            {board.workspaces.map((workspace) => (
              <article className="workspace" key={workspace.workspace_id}>
                <div className="section-heading">
                  <div><p className="eyebrow">Workspace {workspace.number}</p><h2>{workspace.label || workspace.workspace_id} {workspace.focused && <span className="focused">focused</span>}</h2></div>
                  <span>{workspace.tab_count} tabs · {workspace.pane_count} panes · {workspace.agent_status || '—'}</span>
                </div>
                {workspace.tabs.map((tab) => (
                  <section className="tab" key={tab.tab_id}>
                    <h3>Tab {tab.number}: {tab.label || tab.tab_id} {tab.focused && <span className="focused">focused</span>}</h3>
                    <Rows rows={tab.panes} spawning onBanner={onBanner} />
                  </section>
                ))}
              </article>
            ))}
          </section>
          <section className="workspace unplaced">
            <div className="section-heading"><div><p className="eyebrow">Bus-only agents</p><h2>Unplaced</h2></div><span>{board.unplaced.length} agents</span></div>
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

function Exchange({ exchange }: { exchange: TranscriptExchange }) {
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
      {Object.keys(extra).length > 0 && <details><summary>Tool-level detail</summary><pre>{JSON.stringify(extra, null, 2)}</pre></details>}
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

function AgentPanel({ name }: { name: string }) {
  const [agent, setAgent] = useState<AgentDetail | null>(null)
  const [exchanges, setExchanges] = useState<TranscriptExchange[]>([])
  const [cursor, setCursor] = useState('')
  const [hasOlder, setHasOlder] = useState(true)
  const [detail, setDetail] = useState<'exchanges' | 'full'>('exchanges')
  const [problems, setProblems] = useState<Record<string, string>>({ stream: 'Connecting to live transcript…' })
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
    let events: EventSource | null = null
    setAgent(null)
    setExchanges([])
    setCursor('')
    setHasOlder(true)
    setLoadingOlder(false)
    setNotFound(null)
    setProblems({ stream: 'Connecting to live transcript…' })

    const load = async () => {
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

        // Establish the tail before reading the newest window. Anything that
        // lands during initial paint is then present in either the window or
        // the stream, and position de-duplication handles the overlap.
        events = new EventSource(`/api/agents/${encodeURIComponent(name)}/transcript/stream?detail=${detail}`)
        events.onopen = () => setProblems((current) => without(current, 'stream'))
        events.onerror = () => setProblems((current) => ({ ...current, stream: 'Live transcript disconnected; reconnecting…' }))
        events.addEventListener('exchange', (event) => {
          const incoming = JSON.parse(event.data) as TranscriptExchange
          setExchanges((current) => current.some((item) => item.position === incoming.position)
            ? current
            : [...current, incoming].sort((a, b) => a.position - b.position))
        })

        const transcriptResponse = await fetch(`/api/agents/${encodeURIComponent(name)}/transcript?limit=20&detail=${detail}`)
        if (!transcriptResponse.ok) throw new Error((await refusal(transcriptResponse)).detail)
        const page = await transcriptResponse.json() as TranscriptPage
        if (!active) return
        setExchanges((current) => {
          const streamed = new Map(current.map((item) => [item.position, item]))
          page.exchanges.forEach((item) => streamed.set(item.position, item))
          return [...streamed.values()].sort((a, b) => a.position - b.position)
        })
        setCursor(page.cursor)
        setHasOlder(page.exchanges.length > 0)
        setProblems((current) => without(current, 'transcript'))
      } catch (error: unknown) {
        if (active) setProblems((current) => ({ ...current, transcript: error instanceof Error ? error.message : String(error) }))
      }
    }
    void load()
    return () => {
      active = false
      events?.close()
    }
  }, [name, detail])

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
    <main className="panel-scroll agent-page">
      <section className="not-found" role="alert"><p className="eyebrow">404 · {notFound.error}</p><h1>Agent not found</h1><p>{notFound.detail}</p></section>
    </main>
  )

  return (
    <main className="panel-scroll agent-page">
      <header className="agent-header">
        <div><p className="eyebrow">Agent transcript</p><h1>{name}</h1></div>
        <div className="detail-toggle" aria-label="Transcript detail">
          <button className={detail === 'exchanges' ? 'active' : ''} onClick={() => setDetail('exchanges')}>Exchanges</button>
          <button className={detail === 'full' ? 'active' : ''} onClick={() => setDetail('full')}>Full</button>
        </div>
      </header>
      {Object.entries(problems).map(([source, problemDetail]) => <Banner source={source} detail={problemDetail} key={source} />)}
      {agent && <section className="identity" aria-label="Agent identity">
        <dl>
          <div><dt>Tool</dt><dd>{agent.tool}</dd></div>
          <div><dt>Pane</dt><dd>{agent.pane?.pane_id ?? 'unplaced'}</dd></div>
          <div><dt>Herdr</dt><dd>{agent.herdr_status}</dd></div>
          <div><dt>Bus</dt><dd>{agent.bus_status}</dd></div>
          <div><dt>Gap</dt><dd className={agent.gap !== '-' ? 'warning' : ''}>{agent.gap}</dd></div>
        </dl>
      </section>}
      {agent && <ForkControl name={name} onBanner={setLifecycleBanner} />}
      <section className="transcript" aria-label="Transcript">
        <div className="older">
          {hasOlder
            ? <button disabled={!cursor || loadingOlder} onClick={() => void loadOlder()}>{loadingOlder ? 'Loading…' : 'Load older'}</button>
            : <span>Start of transcript</span>}
        </div>
        {exchanges.length === 0 && agent && <p className="empty">No exchanges in this window.</p>}
        {exchanges.map((exchange) => <Exchange exchange={exchange} key={exchange.position} />)}
      </section>
      {agent && <form className="send-box" onSubmit={(event) => void send(event)}>
        <label htmlFor="message">Message {name}</label>
        {readOnly && <div className="read-only" role="alert"><strong>Read-only</strong><span>{readOnly}</span></div>}
        <textarea id="message" rows={4} value={message} disabled={Boolean(readOnly) || sending} onChange={(event) => setMessage(event.target.value)} placeholder="Send an attributed request…" />
        <div className="send-footer">
          <div>{sendProblem && <p className="inline-error" role="alert">{sendProblem}</p>}{sendNotice && <p className="send-notice">{sendNotice}</p>}</div>
          <button type="submit" disabled={!message.trim() || sending || Boolean(readOnly)}>{sending ? 'Sending…' : 'Send request'}</button>
        </div>
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

function PaneTreeRow({ pane, focused, selected, setRef, onFocus, onOpen, onKeyDown }: {
  pane: Pane | Row
  focused: boolean
  selected: boolean
  setRef: (node: HTMLDivElement | null) => void
  onFocus: () => void
  onOpen: () => void
  onKeyDown: (event: React.KeyboardEvent) => void
}) {
  const hasAgent = pane.agent !== '-'
  const name = hasAgent ? pane.agent : ('label' in pane && pane.label) || pane.pane_id
  return <div
    ref={setRef}
    role="treeitem"
    aria-level={2}
    aria-selected={hasAgent && selected}
    tabIndex={focused ? 0 : -1}
    className={`tree-row pane-row${hasAgent ? ' agent-row' : ' shell-row'}${focused ? ' focused' : ''}`}
    onFocus={onFocus}
    onClick={onOpen}
    onKeyDown={onKeyDown}
  >
    <span className={`status-dot ${statusClass(pane.herdr_status)}`} aria-label={`Herdr ${pane.herdr_status}`} />
    <span className="tree-name">{name}</span>
    <span className="tree-tool">{hasAgent ? pane.tool : 'shell'}</span>
    <span className="bus-status">{pane.bus_status !== '-' ? pane.bus_status : 'no bus'}</span>
    {pane.gap !== '-' && <span className="gap-badge">{pane.gap}</span>}
  </div>
}

function FleetSidebar({ board, problems, activeAgent, onOpenAgent }: {
  board: Board | null
  problems: Record<string, string>
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
        <span className="tree-name">{workspace.label || workspace.workspace_id}</span>
        <span className="count-badge">{workspace.pane_count}</span>
      </div>
      {expanded[item.id] && <div role="group">
        {workspace.tabs.map((tab: Tab) => <div className="tab-tree-group" key={tab.tab_id}>
          <div className="tree-tab-separator" role="presentation"><span>{tab.label || `Tab ${tab.number}`}</span></div>
          {tab.panes.map((pane) => {
            const paneItem: TreeItem = { id: `pane:${pane.pane_id}`, kind: 'pane', parent: item.id, agent: pane.agent !== '-' ? pane.agent : undefined }
            return <PaneTreeRow
              key={pane.pane_id}
              pane={pane}
              focused={focusID === paneItem.id}
              selected={activeAgent === paneItem.agent}
              setRef={(node) => { if (node) refs.current.set(paneItem.id, node); else refs.current.delete(paneItem.id) }}
              onFocus={() => setFocusID(paneItem.id)}
              onOpen={() => { setFocusID(paneItem.id); if (paneItem.agent) onOpenAgent(paneItem.agent) }}
              onKeyDown={keyDown(paneItem)}
            />
          })}
        </div>)}
      </div>}
    </div>
  }

  const unplacedItem: TreeItem = { id: 'unplaced', kind: 'unplaced' }
  return <aside className="fleet-sidebar" aria-label="Fleet sidebar">
    <div className="sidebar-heading"><p className="eyebrow">Live substrate</p><h2>Fleet</h2></div>
    {Object.entries(problems).map(([source, detail]) => <div className="sidebar-banner" role="alert" key={source}><strong>{source}</strong><span>{detail}</span></div>)}
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
  const tabRefs = useRef(new Map<string, HTMLButtonElement>())
  const { board, problems, messages, setLifecycleBanner } = useFleet()
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
      <FleetSidebar board={board} problems={problems} activeAgent={active.kind === 'agent' ? active.name : undefined} onOpenAgent={(name) => activate(agentTab(name))} />
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
          >{tab.label}</button>
          {tab.kind === 'agent' && <button className="close-tab" aria-label={`Close ${tab.label}`} onClick={() => close(tab.id)}>×</button>}
        </div>)}
      </div>
      <div className="shell-banners">{Object.entries(problems).map(([source, detail]) => <Banner source={source} detail={detail} key={source} />)}</div>
      <div className="panel-host">
        {tabs.map((tab, index) => <div id={`shell-panel-${index}`} role="tabpanel" aria-labelledby={`shell-tab-${index}`} hidden={tab.id !== activeTab} className="hosted-panel" key={tab.id}>
          {tab.kind === 'board'
            ? <BoardPanel board={board} messages={messages} onBanner={setLifecycleBanner} />
            : <AgentPanel name={tab.name} />}
        </div>)}
      </div>
    </section>
  </div>
}

export default function App() {
  const route = currentRoute()
  if (route.page !== 'missing') return <Shell initialRoute={route} />
  return <main className="agent-page"><AppLink to="/" className="back-link">← Fleet board</AppLink><section className="not-found"><p className="eyebrow">404</p><h1>Page not found</h1></section></main>
}
