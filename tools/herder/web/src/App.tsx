import { useEffect, useState } from 'react'
import type { Board, Pane, Row, SubstrateEvent } from './types'

const columns: Array<[keyof Row, string]> = [
  ['pane_id', 'Pane'],
  ['agent', 'Agent'],
  ['tool', 'Tool'],
  ['herdr_status', 'Herdr status'],
  ['bus_status', 'Bus status'],
  ['gap', 'Gap'],
]

function without(problem: Record<string, string>, key: string) {
  const next = { ...problem }
  delete next[key]
  return next
}

function Rows({ rows }: { rows: Array<Row | Pane> }) {
  return (
    <div className="table-wrap">
      <table>
        <thead><tr>{columns.map(([key, label]) => <th key={key}>{label}</th>)}</tr></thead>
        <tbody>
          {rows.map((row) => (
            <tr key={`${row.pane_id}:${row.agent}`} className={row.gap !== '-' ? 'has-gap' : undefined}>
              {columns.map(([key]) => <td key={key} data-label={key}>{row[key]}</td>)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default function App() {
  const [board, setBoard] = useState<Board | null>(null)
  const [problems, setProblems] = useState<Record<string, string>>({ stream: 'Connecting to live fleet…' })
  const [messages, setMessages] = useState(0)

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
        if (!response.ok) throw new Error((await response.json() as { detail?: string }).detail ?? `HTTP ${response.status}`)
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
        <div className="banner" role="alert" key={source}><strong>{source}</strong><span>{detail}</span></div>
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
                    <Rows rows={tab.panes} />
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
