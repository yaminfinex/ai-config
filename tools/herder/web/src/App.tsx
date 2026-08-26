import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { hotkeysCoreFeature, selectionFeature, syncDataLoaderFeature } from '@headless-tree/core'
import { useTree } from '@headless-tree/react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { isComposerSendShortcut, persistComposerDraft, readComposerDraft } from './composerState'
import { duplicateHcomDeliveryIndices, stripWebOperatorNote } from './messagePolish'
import type {
  AgentDetail,
  Board,
  EntriesPage,
  LifecycleResult,
  Pane,
  Refusal,
  Row,
  SubstrateEvent,
  TranscriptEntry,
} from './types'

const layoutKey = 'herder.web.layout.v1'
const boardTab = { id: 'board', kind: 'board' as const, label: 'Board' }
const defaultSidebarWidth = 250
const entryWindowLimit = 500
const emptyExpandedItems: string[] = []

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
    const names = [...new Set(stored.openTabs)]
    const tabs = [boardTab, ...names.map(agentTab)]
    if (!tabs.some((tab) => tab.id === stored.activeTab)) throw new Error('invalid active tab')
    const activeTab = stored.activeTab
    const sidebarWidth = clampSidebarWidth(stored.sidebarWidth)
    const expandedItems = stored.expandedItems === undefined
      ? null
      : Array.isArray(stored.expandedItems) && stored.expandedItems.every((id) => typeof id === 'string')
        ? [...new Set(stored.expandedItems)]
        : null
    const knownWorkspaceItems = stored.knownWorkspaceItems === undefined
      ? null
      : Array.isArray(stored.knownWorkspaceItems) && stored.knownWorkspaceItems.every((id) => typeof id === 'string')
        ? [...new Set(stored.knownWorkspaceItems)]
        : null
    return { tabs, activeTab, sidebarWidth, expandedItems, knownWorkspaceItems }
  } catch {
    return {
      tabs: [boardTab],
      activeTab: boardTab.id,
      sidebarWidth: defaultSidebarWidth,
      expandedItems: null,
      knownWorkspaceItems: null,
    }
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
  const [transcriptEvents, setTranscriptEvents] = useState<Record<string, number>>({})
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
      // Entry frames wake a bounded nextOffset read. The endpoint remains the
      // source of truth, so reconnect and a burst of frames share one path.
      names.forEach((name) => events?.addEventListener(`entry:${name}`, () => {
        touch()
        setTranscriptEvents((current) => ({ ...current, [name]: (current[name] ?? 0) + 1 }))
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

type ObjectValue = Record<string, unknown>

function objectValue(value: unknown): ObjectValue {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as ObjectValue : {}
}

function valueText(value: unknown): string {
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

function messageText(payload: unknown): string {
  const content = objectValue(objectValue(payload).message).content
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content.map((block) => {
    const part = objectValue(block)
    return valueText(part.text ?? part.thinking)
  }).filter(Boolean).join('\n')
}

function formatDuration(milliseconds: number) {
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '—'
  if (milliseconds < 1000) return `${Math.round(milliseconds)}ms`
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)}s`
  return `${Math.floor(milliseconds / 60_000)}m ${Math.round(milliseconds % 60_000 / 1000)}s`
}

function relativeTime(timestamp: string | undefined, now: number) {
  if (!timestamp) return 'time unknown'
  const delta = now - Date.parse(timestamp)
  if (!Number.isFinite(delta)) return timestamp
  const future = delta < 0
  const elapsed = Math.abs(delta)
  const amount = elapsed < 60_000 ? Math.max(1, Math.round(elapsed / 1000))
    : elapsed < 3_600_000 ? Math.round(elapsed / 60_000)
      : elapsed < 86_400_000 ? Math.round(elapsed / 3_600_000)
        : Math.round(elapsed / 86_400_000)
  const unit = elapsed < 60_000 ? 's' : elapsed < 3_600_000 ? 'm' : elapsed < 86_400_000 ? 'h' : 'd'
  return future ? `in ${amount}${unit}` : `${amount}${unit} ago`
}

function Timestamp({ timestamp, now, absolute = false }: { timestamp?: string, now: number, absolute?: boolean }) {
  if (!timestamp) return <span className="entry-time" title="Timestamp unavailable">time unknown</span>
  const absoluteText = new Date(timestamp).toLocaleString()
  return <time className="entry-time" dateTime={timestamp} title={absoluteText}>{absolute ? `${absoluteText} · ` : ''}{relativeTime(timestamp, now)}</time>
}

function toolSummary(name: string, input: ObjectValue) {
  const preferred = name === 'Bash' ? input.command
    : ['Edit', 'Write', 'Read'].includes(name) ? (input.file_path ?? input.path ?? input.file)
      : undefined
  if (valueText(preferred)) return valueText(preferred).replace(/\s+/g, ' ')
  for (const value of Object.values(input)) {
    const text = valueText(value).replace(/\s+/g, ' ')
    if (text) return text
  }
  return 'no input summary'
}

function resultText(content: unknown) {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return content == null ? '' : JSON.stringify(content, null, 2)
  return content.map((block) => {
    const item = objectValue(block)
    if (item.type === 'image') return '[image result]'
    return valueText(item.text ?? item.raw)
  }).filter(Boolean).join('\n')
}

function structuredPatch(result: ObjectValue) {
  const toolUseResult = objectValue(result.toolUseResult)
  const patches = Array.isArray(toolUseResult.structuredPatch) ? toolUseResult.structuredPatch : []
  return patches.flatMap((patch) => {
    const lines = objectValue(patch).lines
    return Array.isArray(lines) ? lines.filter((line): line is string => typeof line === 'string') : []
  })
}

function ToolEntry({ entry, result, now }: { entry: TranscriptEntry, result?: TranscriptEntry, now: number }) {
  const call = objectValue(entry.payload)
  const outcome = objectValue(result?.payload)
  const name = valueText(call.name) || 'unknown tool'
  const input = objectValue(call.input)
  const callTime = entry.timestamp ? Date.parse(entry.timestamp) : NaN
  const resultTime = result?.timestamp ? Date.parse(result.timestamp) : NaN
  const duration = result ? formatDuration(resultTime - callTime) : 'running · no result yet'
  const patch = structuredPatch(outcome)
  const output = resultText(outcome.content)
  const imageCount = Number(outcome.image_count ?? 0)
  return <details className="entry-expander tool-entry">
    <summary>
      <span className={`tool-status ${result ? outcome.is_error === true ? 'error' : 'success' : 'running'}`} />
      <strong>{name}</strong><span className="tool-summary">{toolSummary(name, input)}</span>
      <span className="tool-duration">{duration}</span><Timestamp timestamp={result?.timestamp ?? entry.timestamp} now={now} />
    </summary>
    <div className="entry-detail">
      <h4>Input</h4><pre>{JSON.stringify(input, null, 2)}</pre>
      {result && <><h4>Output</h4>{output && <pre>{output}</pre>}
        {imageCount > 0 && <div className="image-placeholder">▧ {imageCount} image result{imageCount === 1 ? '' : 's'} present (not served)</div>}
        {outcome.truncated === true && <div className="truncation-banner">Output capped at 16 KiB — {Number(outcome.total_bytes).toLocaleString()} bytes total.</div>}
        {patch.length > 0 && <><h4>Structured patch</h4><pre className="diff-output">{patch.map((line, index) => <span className={line.startsWith('+') ? 'diff-add' : line.startsWith('-') ? 'diff-delete' : ''} key={`${index}:${line}`}>{line}{'\n'}</span>)}</pre></>}
      </>}
    </div>
  </details>
}

type EntryRelationships = {
  toolResults: Map<string, TranscriptEntry>
  pairedToolResults: Set<number>
  pairedDeliveries: Set<number>
  commandOutputs: Map<number, TranscriptEntry>
  pairedCommandOutputs: Set<number>
  duplicateHcomDeliveries: Map<number, Set<number>>
}

function relateEntries(entries: TranscriptEntry[]): EntryRelationships {
  const toolResults = new Map<string, TranscriptEntry>()
  const toolUses = new Set<string>()
  entries.forEach((entry) => {
    const id = valueText(objectValue(entry.payload).tool_use_id)
    if (entry.kind === 'tool_result' && id) toolResults.set(id, entry)
    if (entry.kind === 'tool_use' && id) toolUses.add(id)
  })
  const pairedToolResults = new Set(entries.flatMap((entry, index) =>
    entry.kind === 'tool_result' && toolUses.has(valueText(objectValue(entry.payload).tool_use_id)) ? [index] : []))
  const pairedDeliveries = new Set<number>()
  entries.forEach((entry, index) => {
    if (entry.kind === 'hcom_delivery_stub' && entries[index + 1]?.kind === 'hcom_delivery') pairedDeliveries.add(index + 1)
  })
  const commandOutputs = new Map<number, TranscriptEntry>()
  const pairedCommandOutputs = new Set<number>()
  entries.forEach((entry, index) => {
    const next = entries[index + 1]
    if (entry.kind === 'command_stdout' && messageText(entry.payload).includes('<command-name>') &&
      next?.kind === 'command_stdout' && messageText(next.payload).includes('<local-command-stdout>')) {
      commandOutputs.set(index, next)
      pairedCommandOutputs.add(index + 1)
    }
  })
  return {
    toolResults, pairedToolResults, pairedDeliveries, commandOutputs, pairedCommandOutputs,
    duplicateHcomDeliveries: duplicateHcomDeliveryIndices(entries),
  }
}

function HcomCards({ entry, entryIndex, now, showSystem, relationships }: {
  entry: TranscriptEntry
  entryIndex: number
  now: number
  showSystem: boolean
  relationships: EntryRelationships
}) {
  const deliveries = objectValue(entry.payload).deliveries
  const values = Array.isArray(deliveries) ? deliveries : []
  const duplicates = relationships.duplicateHcomDeliveries.get(entryIndex)
  const visibleValues = values.flatMap((raw, index) => duplicates?.has(index) ? [] : [{ raw, index }])
  if (visibleValues.length === 0) return null
  const parsed = visibleValues.some(({ raw }) => {
    const delivery = objectValue(raw)
    return Boolean(valueText(delivery.sender) && valueText(delivery.message_id))
  })
  if (!parsed) {
    if (!showSystem) return null
    return <details className="system-chip unknown-entry"><summary>unparsed hook attachment · <Timestamp timestamp={entry.timestamp} now={now} /></summary><pre>{visibleValues.map(({ raw }) => valueText(objectValue(raw).text)).filter(Boolean).join('\n')}</pre></details>
  }
  return <>{visibleValues.map(({ raw, index }) => {
    const delivery = objectValue(raw)
    return <article className="entry-card hcom-card" key={`${entry.uuid ?? entry.byteOffset}:${index}`}>
      <header><strong>{valueText(delivery.sender) || 'unknown sender'}</strong><span>→ {valueText(delivery.recipient) || 'unknown recipient'}</span>
        {valueText(delivery.intent) && <span className={`intent-badge ${valueText(delivery.intent)}`}>{valueText(delivery.intent)}</span>}
        {valueText(delivery.message_id) && <span className="message-id">#{valueText(delivery.message_id)}</span>}
        {valueText(delivery.thread) && <span className="thread-chip">{valueText(delivery.thread)}</span>}
        <Timestamp timestamp={entry.timestamp} now={now} />
      </header><div>{stripWebOperatorNote(valueText(delivery.text)) || '(delivery body unavailable)'}</div>
    </article>
  })}</>
}

function EntryView({ entry, index, entries, relationships, agentName, now, showSystem }: {
  entry: TranscriptEntry
  index: number
  entries: TranscriptEntry[]
  relationships: EntryRelationships
  agentName: string
  now: number
  showSystem: boolean
}) {
  const payload = objectValue(entry.payload)
  const content = messageText(entry.payload)
  if (relationships.pairedToolResults.has(index) || relationships.pairedDeliveries.has(index) || relationships.pairedCommandOutputs.has(index)) return null
  if (entry.kind === 'hcom_delivery_stub') {
    const delivery = entries[index + 1]
    return delivery?.kind === 'hcom_delivery' ? <HcomCards entry={delivery} entryIndex={index + 1} now={now} showSystem={showSystem} relationships={relationships} />
      : <div className="system-chip">hcom delivery pending attachment · <Timestamp timestamp={entry.timestamp} now={now} /></div>
  }
  if (entry.kind === 'hcom_delivery') return <HcomCards entry={entry} entryIndex={index} now={now} showSystem={showSystem} relationships={relationships} />
  if (entry.kind === 'tool_use') {
    return <ToolEntry entry={entry} result={relationships.toolResults.get(valueText(payload.tool_use_id))} now={now} />
  }
  if (entry.kind === 'tool_result') {
    return <details className="entry-expander tool-entry"><summary><span className={`tool-status ${payload.is_error === true ? 'error' : 'success'}`} /><strong>unpaired tool result</strong><Timestamp timestamp={entry.timestamp} now={now} /></summary><div className="entry-detail"><pre>{resultText(payload.content)}</pre></div></details>
  }
  if (entry.kind === 'assistant_text') return <article className="assistant-entry"><header><strong>{agentName}</strong><Timestamp timestamp={entry.timestamp} now={now} /></header><div className="markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown></div></article>
  if (entry.kind === 'thinking') {
    const nextTime = entries.slice(index + 1).find((candidate) => candidate.timestamp)?.timestamp
    const duration = nextTime && entry.timestamp ? formatDuration(Date.parse(nextTime) - Date.parse(entry.timestamp)) : 'duration unknown'
    return <details className="entry-expander thinking-entry"><summary>thinking · {duration}<Timestamp timestamp={entry.timestamp} now={now} /></summary><div className="entry-detail">{content || 'Thinking content unavailable.'}</div></details>
  }
  if (entry.kind === 'human_prompt') return <article className="entry-card human-entry"><header><strong>owner (terminal)</strong><Timestamp timestamp={entry.timestamp} now={now} absolute /></header><div>{content || 'Prompt body unavailable.'}</div></article>
  if (entry.kind === 'command_stdout') {
    const command = content.match(/<command-name>([\s\S]*?)<\/command-name>/)?.[1] ?? 'slash command'
    const args = content.match(/<command-args>([\s\S]*?)<\/command-args>/)?.[1] ?? ''
    const outputContent = messageText(relationships.commandOutputs.get(index)?.payload) || content
    const output = outputContent.match(/<local-command-stdout>([\s\S]*?)<\/local-command-stdout>/)?.[1] ?? ''
    return <details className="entry-expander command-entry"><summary><span className="command-chip">{command}</span>{args && <span className="tool-summary">{args}</span>}<Timestamp timestamp={relationships.commandOutputs.get(index)?.timestamp ?? entry.timestamp} now={now} /></summary>{(args || output) && <div className="entry-detail">{args && <><h4>Arguments</h4><pre>{args}</pre></>}{output && <><h4>Output</h4><pre>{output}</pre></>}</div>}</details>
  }
  if (entry.kind === 'compact_divider') {
    const metadata = objectValue(payload.compactMetadata)
    if (Object.keys(metadata).length > 0) return <div className="compaction-divider"><span>context compacted ({valueText(metadata.trigger) || 'unknown'}, {Number(metadata.preTokens).toLocaleString()} → {Number(metadata.postTokens).toLocaleString()} tokens)</span><Timestamp timestamp={entry.timestamp} now={now} /></div>
    return <details className="entry-expander compact-summary"><summary>compaction summary<Timestamp timestamp={entry.timestamp} now={now} /></summary><div className="entry-detail markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{content || valueText(payload.content)}</ReactMarkdown></div></details>
  }
  if (entry.kind === 'turn_duration') return <div className="turn-footer">turn · {formatDuration(Number(payload.durationMs))}{payload.messageCount != null ? ` · ${Number(payload.messageCount)} messages` : ''} · <Timestamp timestamp={entry.timestamp} now={now} /></div>
  if (entry.kind === 'task_notification' || entry.kind === 'injected_system') return <details className="system-chip"><summary>{entry.kind === 'task_notification' ? 'background task finished' : 'injected system prompt'} · <Timestamp timestamp={entry.timestamp} now={now} /></summary><div>{content || JSON.stringify(payload)}</div></details>
  if (entry.kind === 'system_chip') {
    if (!showSystem && payload.subtype !== 'scheduled_task_fire') return null
    return <details className="system-chip"><summary>{valueText(payload.subtype) || 'system entry'} · <Timestamp timestamp={entry.timestamp} now={now} /></summary><pre>{JSON.stringify(payload, null, 2)}</pre></details>
  }
  if (!showSystem) return null
  return <details className="system-chip unknown-entry"><summary>{entry.quarantine ? `quarantined entry · ${entry.quarantine.reason}` : `unknown entry · ${entry.kind}`} · <Timestamp timestamp={entry.timestamp} now={now} /></summary><pre>{JSON.stringify(entry.payload, null, 2)}</pre></details>
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

function AgentPanel({ name, onViewer, identityReadOnly, liveWake, streamGeneration, resetGeneration }: {
  name: string
  onViewer: (viewer: string) => void
  identityReadOnly: string
  liveWake: number
  streamGeneration: number
  resetGeneration: number
}) {
  const [agent, setAgent] = useState<AgentDetail | null>(null)
  const [entries, setEntries] = useState<TranscriptEntry[]>([])
  const [sessionId, setSessionId] = useState('')
  const [nextOffset, setNextOffset] = useState<number | null>(null)
  const [showSystem, setShowSystem] = useState(false)
  const [problems, setProblems] = useState<Record<string, string>>({})
  const [notFound, setNotFound] = useState<Refusal | null>(null)
  const [now, setNow] = useState(Date.now())
  const [following, setFollowing] = useState(true)
  const [newEntryCount, setNewEntryCount] = useState(0)
  const [reloadGeneration, setReloadGeneration] = useState(0)
  const [appendGeneration, setAppendGeneration] = useState(0)
  const [message, setMessage] = useState(() => readComposerDraft(localStorage, name))
  const [sendProblem, setSendProblem] = useState('')
  const [sendNotice, setSendNotice] = useState('')
  const [sending, setSending] = useState(false)
  const [readOnly, setReadOnly] = useState('')
  const transcriptRef = useRef<HTMLElement>(null)
  const composerRef = useRef<HTMLTextAreaElement>(null)
  const followingRef = useRef(true)
  const reanchorAfterAppendRef = useRef(true)
  const effectiveReadOnly = identityReadOnly || readOnly
  const setLifecycleBanner = (key: string, problemDetail: string) => setProblems((current) => problemDetail
    ? { ...current, [key]: problemDetail }
    : without(current, key))

  useEffect(() => {
    let active = true
    setAgent(null)
    setEntries([])
    setSessionId('')
    setNextOffset(null)
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
  }, [name])

  useEffect(() => {
    let active = true
    const loadWindow = async () => {
      try {
        const response = await fetch(`/api/agents/${encodeURIComponent(name)}/entries?limit=${entryWindowLimit}`)
        if (!response.ok) throw new Error((await refusal(response)).detail)
        const page = await response.json() as EntriesPage
        if (!active) return
        setEntries(page.entries ?? [])
        setSessionId(page.sessionId)
        setNextOffset(page.nextOffset ?? null)
        followingRef.current = true
        reanchorAfterAppendRef.current = true
        setFollowing(true)
        setNewEntryCount(0)
        setAppendGeneration((generation) => generation + 1)
        setProblems((current) => without(current, 'transcript'))
      } catch (error: unknown) {
        if (active) setProblems((current) => ({ ...current, transcript: error instanceof Error ? error.message : String(error) }))
      }
    }
    void loadWindow()
    return () => { active = false }
  }, [name, resetGeneration, reloadGeneration])

  useEffect(() => {
    if (!sessionId || nextOffset === null || streamGeneration === 0) return
    let active = true
    const follow = async () => {
      let offset = nextOffset
      try {
        for (;;) {
          const query = new URLSearchParams({ from: String(offset), limit: String(entryWindowLimit), sessionId })
          const response = await fetch(`/api/agents/${encodeURIComponent(name)}/entries?${query}`)
          if (!response.ok) throw new Error((await refusal(response)).detail)
          const page = await response.json() as EntriesPage
          if (!active) return
          if (page.reset) {
            setSessionId('')
            setNextOffset(null)
            setEntries([])
            setProblems((current) => ({ ...current, transcript: `Transcript reset: ${page.reset?.reason}. Reloading the current session…` }))
            setReloadGeneration((generation) => generation + 1)
            return
          }
          const incoming = page.entries ?? []
          if (incoming.length > 0) {
            const wasAtBottom = followingRef.current
            reanchorAfterAppendRef.current = wasAtBottom
            setEntries((current) => {
              const seen = new Set(current.map((entry) => entry.uuid || `offset:${entry.byteOffset}`))
              return [...current, ...incoming.filter((entry) => !seen.has(entry.uuid || `offset:${entry.byteOffset}`))].slice(-entryWindowLimit)
            })
            if (!wasAtBottom) setNewEntryCount((count) => count + incoming.length)
            setAppendGeneration((generation) => generation + 1)
          }
          const advanced = page.nextOffset ?? offset
          setNextOffset(advanced)
          setProblems((current) => without(current, 'transcript'))
          if (incoming.length < entryWindowLimit || advanced === offset) break
          offset = advanced
        }
      } catch (error: unknown) {
        if (active) setProblems((current) => ({ ...current, transcript: error instanceof Error ? error.message : String(error) }))
      }
    }
    void follow()
    return () => { active = false }
    // liveWake is the append signal; reconnect also catches anything missed.
  }, [name, sessionId, liveWake, streamGeneration, resetGeneration])

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    persistComposerDraft(localStorage, name, message)
  }, [message, name])

  useLayoutEffect(() => {
    const composer = composerRef.current
    if (!composer) return
    composer.style.height = '0px'
    const height = Math.min(composer.scrollHeight, 160)
    composer.style.height = `${height}px`
    composer.style.overflowY = composer.scrollHeight > 160 ? 'auto' : 'hidden'
  }, [message])

  useLayoutEffect(() => {
    if (!reanchorAfterAppendRef.current) return
    const transcript = transcriptRef.current
    if (transcript) transcript.scrollTop = transcript.scrollHeight
    followingRef.current = true
    reanchorAfterAppendRef.current = false
    setFollowing(true)
    setNewEntryCount(0)
  }, [appendGeneration])

  const relationships = useMemo(() => relateEntries(entries), [entries])

  const send = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!message.trim() || sending || effectiveReadOnly) return
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
      persistComposerDraft(localStorage, name, '')
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
        <div className="agent-actions"><label className="system-toggle"><input type="checkbox" checked={showSystem} onChange={(event) => setShowSystem(event.target.checked)} /> show system entries</label><span className={`follow-chip${following ? '' : ' paused'}`}>{following ? 'follow ✓' : 'follow paused'}</span>{agent && <ForkControl name={name} onBanner={setLifecycleBanner} />}</div>
      </header>
      {Object.entries(problems).map(([source, problemDetail]) => <Banner source={source} detail={problemDetail} key={source} />)}
      <section className="transcript" aria-label="Transcript" ref={transcriptRef} onScroll={(event) => {
        const node = event.currentTarget
        const atBottom = node.scrollHeight - node.scrollTop - node.clientHeight < 48
        followingRef.current = atBottom
        setFollowing(atBottom)
        if (atBottom) setNewEntryCount(0)
      }}>
        <div className="window-note">Showing the latest {entries.length} classified entries · live from byte {nextOffset ?? '…'}</div>
        {entries.length === 0 && agent && <p className="empty">No renderable entries in this window.</p>}
        {entries.map((entry, index) => <EntryView entry={entry} index={index} entries={entries} relationships={relationships} agentName={name} now={now} showSystem={showSystem} key={entry.uuid || `${entry.byteOffset}:${entry.line}`} />)}
        {newEntryCount > 0 && <button className="jump-latest" onClick={() => {
          const transcript = transcriptRef.current
          if (transcript) transcript.scrollTop = transcript.scrollHeight
          followingRef.current = true
          setFollowing(true)
          setNewEntryCount(0)
        }}>↓ {newEntryCount} new</button>}
      </section>
      {agent && <form className="send-box" onSubmit={(event) => void send(event)}>
        {effectiveReadOnly && <div className="read-only" role="alert"><strong>Read-only</strong><span>{effectiveReadOnly}</span></div>}
        <label htmlFor="message">Message {name} <span>· Enter for newline · Ctrl/Cmd+Enter to send</span></label><textarea
          id="message"
          ref={composerRef}
          rows={1}
          value={message}
          disabled={Boolean(effectiveReadOnly) || sending}
          onChange={(event) => setMessage(event.target.value)}
          onKeyDown={(event) => {
            if (!isComposerSendShortcut(event) || event.nativeEvent.isComposing) return
            event.preventDefault()
            event.currentTarget.form?.requestSubmit()
          }}
          placeholder="Send an attributed request…"
        />
        <div className="send-footer">
          <div>{sendProblem && <p className="inline-error" role="alert">{sendProblem}</p>}{sendNotice && <p className="send-notice">{sendNotice}</p>}</div>
          <button type="submit" disabled={!message.trim() || sending || Boolean(effectiveReadOnly)}>{sending ? 'Sending…' : 'Send request'}</button>
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

function statusClass(status: string) {
  if (status === 'working' || status === 'active') return 'working'
  if (status === 'idle' || status === 'listening') return 'idle'
  if (status === 'dead') return 'dead'
  return 'unknown'
}

type SidebarNode = {
  id: string
  kind: 'root' | 'workspace' | 'pane' | 'unplaced'
  name: string
  children: string[]
  count?: number
  pane?: Pane | Row
  tabLabel?: string
}

function FleetSidebar({ board, activeAgent, onOpenAgent, expandedItems, onExpandedItems, knownWorkspaceItems, onKnownWorkspaceItems }: {
  board: Board | null
  activeAgent?: string
  onOpenAgent: (name: string) => void
  expandedItems: string[] | null
  onExpandedItems: (items: string[]) => void
  knownWorkspaceItems: string[] | null
  onKnownWorkspaceItems: (items: string[]) => void
}) {
  const [selectedItems, setSelectedItems] = useState<string[]>([])

  const nodes = useMemo(() => {
    const result = new Map<string, SidebarNode>()
    const root: SidebarNode = { id: 'tree-root', kind: 'root', name: 'Fleet', children: [] }
    result.set(root.id, root)
    if (!board) return result

    const workspaces = new Map(board.workspaces.map((workspace) => [workspace.workspace_id, workspace]))
    const workspaceChildren = new Map<string, string[]>()
    board.workspaces.forEach((workspace) => workspaceChildren.set(workspace.workspace_id, []))
    board.workspaces.forEach((workspace) => {
      const id = `workspace:${workspace.workspace_id}`
      if (workspace.worktree_of && workspaces.has(workspace.worktree_of)) {
        workspaceChildren.get(workspace.worktree_of)?.push(id)
      } else {
        root.children.push(id)
      }
    })
    board.workspaces.forEach((workspace) => {
      const id = `workspace:${workspace.workspace_id}`
      const children: string[] = []
      workspace.tabs.forEach((tab) => tab.panes.forEach((pane) => {
        const paneID = `pane:${pane.pane_id}`
        children.push(paneID)
        result.set(paneID, {
          id: paneID,
          kind: 'pane',
          name: pane.agent !== '-' ? pane.agent : pane.label || pane.pane_id,
          children: [],
          pane,
          tabLabel: `tab ${tab.number}: ${tab.label || tab.tab_id}`,
        })
      }))
      children.push(...(workspaceChildren.get(workspace.workspace_id) ?? []))
      result.set(id, {
        id,
        kind: 'workspace',
        name: workspaceName(workspace.label, workspace.workspace_id),
        children,
        count: workspace.pane_count,
      })
    })
    const unplaced: SidebarNode = { id: 'unplaced', kind: 'unplaced', name: 'Unplaced', children: [], count: board.unplaced.length }
    board.unplaced.forEach((row) => {
      const id = `unplaced:${row.agent}`
      unplaced.children.push(id)
      result.set(id, { id, kind: 'pane', name: row.agent, children: [], pane: row })
    })
    root.children.push(unplaced.id)
    result.set(unplaced.id, unplaced)
    return result
  }, [board])

  useEffect(() => {
    if (!board) return
    const workspaceItems = [...nodes.values()].filter((node) => node.kind === 'workspace').map((node) => node.id)
    if (expandedItems === null) {
      onExpandedItems([...nodes.values()].filter((node) => node.kind === 'workspace' || node.kind === 'unplaced').map((node) => node.id))
      onKnownWorkspaceItems(workspaceItems)
      return
    }
    if (knownWorkspaceItems === null) {
      // Layouts saved before knownWorkspaceItems existed keep their explicit
      // expansion state; subsequent workspace arrivals can then default open.
      onKnownWorkspaceItems(workspaceItems)
      return
    }
    const known = new Set(knownWorkspaceItems)
    const unseen = workspaceItems.filter((id) => !known.has(id))
    if (unseen.length === 0) return
    onExpandedItems([...new Set([...expandedItems, ...unseen])])
    onKnownWorkspaceItems([...new Set([...knownWorkspaceItems, ...workspaceItems])])
  }, [board, expandedItems, knownWorkspaceItems, nodes, onExpandedItems, onKnownWorkspaceItems])

  useEffect(() => {
    if (!activeAgent) {
      setSelectedItems([])
      return
    }
    const match = [...nodes.values()].find((node) => node.kind === 'pane' && node.pane?.agent === activeAgent)
    setSelectedItems(match ? [match.id] : [])
  }, [activeAgent, nodes])

  const tree = useTree<SidebarNode>({
    rootItemId: 'tree-root',
    getItemName: (item) => item.getItemData().name,
    isItemFolder: (item) => item.getItemData().children.length > 0,
    dataLoader: {
      getItem: (id) => nodes.get(id) ?? nodes.get('tree-root')!,
      getChildren: (id) => nodes.get(id)?.children ?? [],
    },
    state: { expandedItems: expandedItems ?? emptyExpandedItems, selectedItems },
    setExpandedItems: (update) => onExpandedItems(typeof update === 'function' ? update(expandedItems ?? emptyExpandedItems) : update),
    setSelectedItems: (update) => setSelectedItems((current) => typeof update === 'function' ? update(current) : update),
    onPrimaryAction: (item) => {
      const node = item.getItemData()
      if (node.pane?.agent && node.pane.agent !== '-') onOpenAgent(node.pane.agent)
    },
    hotkeys: {
      customPrimaryActionEnter: {
        hotkey: 'Enter',
        preventDefault: true,
        handler: (_event, currentTree) => currentTree.getFocusedItem()?.primaryAction(),
      },
      customPrimaryActionSpace: {
        hotkey: 'Space',
        preventDefault: true,
        handler: (_event, currentTree) => currentTree.getFocusedItem()?.primaryAction(),
      },
    },
    features: [syncDataLoaderFeature, selectionFeature, hotkeysCoreFeature],
  })

  useEffect(() => {
    tree.rebuildTree()
  }, [nodes, tree])

  return <aside className="fleet-sidebar" aria-label="Fleet sidebar">
    <div className="sidebar-heading"><span className="status-dot working" /><strong>Fleet</strong><span>herdr truth</span></div>
    {!board ? <p className="sidebar-loading">Waiting for fleet…</p> : <div {...tree.getContainerProps('Workspaces and agents')} className="fleet-tree">
      {tree.getItems().map((item) => {
        const node = item.getItemData()
        const pane = node.pane
        const signal = pane && pane.agent !== '-' && pane.bus_status !== '-' ? pane.bus_status : ''
        const folder = item.isFolder()
        return <div
          {...item.getProps()}
          key={item.getId()}
          className={`tree-row ${node.kind === 'pane' ? 'pane-row' : 'workspace-row'}${pane?.agent && pane.agent !== '-' ? ' agent-row' : ''}${pane?.agent === '-' ? ' shell-row' : ''}${node.kind === 'unplaced' ? ' unplaced-row' : ''}${item.isFocused() ? ' tree-focused' : ''}${item.isSelected() ? ' selected' : ''}`}
          style={{ paddingLeft: `${item.getItemMeta().level * 16 + 5}px` }}
          title={pane ? `${pane.pane_id}${node.tabLabel ? ` · ${node.tabLabel}` : ''} · ${pane.tool} · herdr ${pane.herdr_status}${signal ? ` · bus ${signal}` : ''}` : node.name}
          onFocus={() => item.setFocused()}
        >
          {folder ? <button
            className={`disclosure${item.isExpanded() ? ' expanded' : ''}`}
            type="button"
            aria-label={`${item.isExpanded() ? 'Collapse' : 'Expand'} ${node.name}`}
            title={`${item.isExpanded() ? 'Collapse' : 'Expand'} ${node.name}`}
            onClick={(event) => {
              event.stopPropagation()
              if (item.isExpanded()) item.collapse()
              else item.expand()
            }}
          ><span aria-hidden="true">›</span></button> : <span className="disclosure-spacer" />}
          {pane && <span className={`status-dot ${statusClass(pane.herdr_status)}`} aria-label={`Herdr ${pane.herdr_status}`} />}
          <span className="tree-name">{node.name}</span>
          {folder && <span className="count-badge">{node.count ?? node.children.length}</span>}
          {signal && <span className="bus-status">{signal}</span>}
          {pane && pane.agent !== '-' && pane.gap !== '-' && <span className="gap-badge">{gapLabel(pane.gap)}</span>}
        </div>
      })}
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
  const [expandedItems, setExpandedItems] = useState<string[] | null>(initial.expandedItems)
  const [knownWorkspaceItems, setKnownWorkspaceItems] = useState<string[] | null>(initial.knownWorkspaceItems)
  const [viewer, setViewer] = useState('resolving…')
  const [viewerState, setViewerState] = useState<'resolving' | 'attributed' | 'unresolved' | 'unavailable'>('resolving')
  const [viewerProblem, setViewerProblem] = useState('')
  const [viewerReadOnly, setViewerReadOnly] = useState('Resolving viewer identity…')
  const tabRefs = useRef(new Map<string, HTMLButtonElement>())
  const agentNames = tabs.flatMap((tab) => tab.kind === 'agent' ? [tab.name] : [])
  const {
    board, problems, messages, lastEvent, setLifecycleBanner,
    streamGeneration, transcriptEvents, transcriptResets,
  } = useFleet(agentNames)
  const active = tabs.find((tab) => tab.id === activeTab) ?? boardTab

  useEffect(() => {
    let activeRequest = true
    const resolveViewer = async () => {
      try {
        const response = await fetch('/api/viewer')
        if (!response.ok) {
          const problem = await refusal(response)
          if (!activeRequest) return
          setViewer('unresolved')
          if (response.status === 409) {
            setViewerState('unresolved')
            setViewerProblem('')
            setViewerReadOnly(`Connect via Tailscale to send. ${problem.detail}`)
          } else {
            setViewerState('unavailable')
            setViewerProblem(problem.detail)
            setViewerReadOnly(`Viewer identity is unavailable. ${problem.detail}`)
          }
          return
        }
        const result = await response.json() as { viewer: string }
        if (!activeRequest) return
        setViewer(result.viewer)
        setViewerState('attributed')
        setViewerProblem('')
        setViewerReadOnly('')
      } catch (error: unknown) {
        if (!activeRequest) return
        setViewer('unresolved')
        setViewerState('unavailable')
        const detail = error instanceof Error ? error.message : String(error)
        setViewerProblem(detail)
        setViewerReadOnly(`Viewer identity is unavailable. ${detail}`)
      }
    }
    void resolveViewer()
    return () => { activeRequest = false }
  }, [])

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
    if (expandedItems !== null) value.expandedItems = expandedItems
    if (knownWorkspaceItems !== null) value.knownWorkspaceItems = knownWorkspaceItems
    try {
      localStorage.setItem(layoutKey, JSON.stringify(value))
    } catch {
      // Viewer persistence is best-effort; the live shell remains usable when
      // browser storage is unavailable or full.
    }
  }, [tabs, activeTab, sidebarWidth, expandedItems, knownWorkspaceItems])

  const close = (id: string) => {
    const index = tabs.findIndex((tab) => tab.id === id)
    const nextTabs = tabs.filter((tab) => tab.id !== id)
    setTabs(nextTabs)
    if (activeTab === id) activate(nextTabs[Math.max(0, index - 1)] ?? boardTab)
  }

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      const command = event.ctrlKey || event.metaKey
      if (command && event.key.toLowerCase() === 'w' && active.kind === 'agent') {
        close(active.id)
      } else if (command && (event.key === 'PageDown' || event.key === 'PageUp')) {
        const index = tabs.findIndex((tab) => tab.id === active.id)
        const delta = event.key === 'PageDown' ? 1 : -1
        activate(tabs[(index + delta + tabs.length) % tabs.length])
      } else if (event.altKey && event.key === '1') {
        document.querySelector<HTMLElement>('.fleet-tree [role="treeitem"]')?.focus()
      } else if (event.altKey && event.key === '2') {
        document.querySelector<HTMLTextAreaElement>('.hosted-panel:not([hidden]) #message')?.focus()
      } else return
      event.preventDefault()
    }
    window.addEventListener('keydown', shortcut)
    return () => window.removeEventListener('keydown', shortcut)
  })
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
      <FleetSidebar
        board={board}
        activeAgent={active.kind === 'agent' ? active.name : undefined}
        onOpenAgent={(name) => activate(agentTab(name))}
        expandedItems={expandedItems}
        onExpandedItems={setExpandedItems}
        knownWorkspaceItems={knownWorkspaceItems}
        onKnownWorkspaceItems={setKnownWorkspaceItems}
      />
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
        {tabs.map((tab, index) => <div role="presentation" className={`shell-tab${tab.id === activeTab ? ' active' : ''}`} key={tab.id} onAuxClick={(event) => { if (event.button === 1 && tab.kind === 'agent') close(tab.id) }}>
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
        <span className="layout-chip" title="Shortcuts: Ctrl/Cmd+W close tab · Ctrl/Cmd+PageUp/PageDown previous/next tab · Alt+1 focus sidebar · Alt+2 focus composer">layout: this browser</span>
      </div>
      <div className="shell-banners">{viewerProblem && <Banner source="viewer" detail={viewerProblem} />}{Object.entries(problems).map(([source, detail]) => <Banner source={source} detail={detail} key={source} />)}</div>
      <div className="panel-host">
        {tabs.map((tab, index) => <div id={`shell-panel-${index}`} role="tabpanel" aria-labelledby={`shell-tab-${index}`} hidden={tab.id !== activeTab} className="hosted-panel" key={tab.id}>
          {tab.kind === 'board'
            ? <BoardPanel board={board} onBanner={setLifecycleBanner} />
            : <AgentPanel
              name={tab.name}
              onViewer={(resolvedViewer) => {
                setViewer(resolvedViewer)
                setViewerState('attributed')
                setViewerProblem('')
                setViewerReadOnly('')
              }}
              identityReadOnly={viewerReadOnly}
              liveWake={transcriptEvents[tab.name] ?? 0}
              streamGeneration={streamGeneration}
              resetGeneration={transcriptResets[tab.name] ?? 0}
            />}
        </div>)}
      </div>
      <footer className="status-bar">
        <span>substrate: herdr {board ? '✓' : '…'} · hcom {problems.hcom ? '×' : '✓'}</span>
        <span className={problems.stream ? 'fault' : ''}>SSE: {problems.stream ? 'reconnecting' : 'connected'}</span>
        <span>viewer: {viewer}</span><span>{viewerState === 'resolving' ? 'resolving identity' : viewerState === 'attributed' ? 'attributed' : viewerState === 'unavailable' ? 'identity unavailable' : 'read-only · unattributed'}</span>
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
