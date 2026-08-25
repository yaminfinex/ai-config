import { useEffect, useRef, useState } from 'react'
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

function BoardPage() {
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

  return (
    <main>
      <header>
        <div><p className="eyebrow">Live substrate</p><h1>Herder fleet</h1></div>
        <div className="ticker" aria-label="Messages seen">{messages} messages seen</div>
      </header>
      {Object.entries(problems).map(([source, detail]) => (
        <Banner source={source} detail={detail} key={source} />
      ))}
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
                    <Rows rows={tab.panes} spawning onBanner={setLifecycleBanner} />
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

function AgentPage({ name }: { name: string }) {
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
    <main className="agent-page">
      <AppLink to="/" className="back-link">← Fleet board</AppLink>
      <section className="not-found" role="alert"><p className="eyebrow">404 · {notFound.error}</p><h1>Agent not found</h1><p>{notFound.detail}</p></section>
    </main>
  )

  return (
    <main className="agent-page">
      <AppLink to="/" className="back-link">← Fleet board</AppLink>
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

function currentRoute(): { page: 'board' } | { page: 'agent', name: string } | { page: 'missing' } {
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

export default function App() {
  const [route, setRoute] = useState(currentRoute)
  useEffect(() => {
    const update = () => setRoute(currentRoute())
    window.addEventListener('popstate', update)
    return () => window.removeEventListener('popstate', update)
  }, [])
  if (route.page === 'board') return <BoardPage />
  if (route.page === 'agent') return <AgentPage name={route.name} />
  return <main className="agent-page"><AppLink to="/" className="back-link">← Fleet board</AppLink><section className="not-found"><p className="eyebrow">404</p><h1>Page not found</h1></section></main>
}
