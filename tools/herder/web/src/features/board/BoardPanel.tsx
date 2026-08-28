import { Fragment } from 'react'
import { AppLink, navigate } from '../../shared/navigation'
import { AgentStatusDot, gapLabel, workspaceName } from '../../shared/presentation'
import { SpawnControl } from '../spawn/SpawnControl'
import { screenPanePresentation } from '../screen/screenPresentation'
import type { Board, Pane, Row } from '../../types'

function RowCells({ row, spawning = false, onBanner = () => {} }: { row: Row | Pane, spawning?: boolean, onBanner?: (key: string, detail: string) => void }) {
  const hasAgent = row.agent !== '-'
  const terminal = hasAgent ? null : screenPanePresentation(row as Pane)
  return <>
    <td className="pane-id">{row.parent_agent ? 'subagent' : row.pane_id}</td>
    <td className="agent-cell">{hasAgent
      ? <><AppLink to={`/agents/${encodeURIComponent(row.agent)}`}>{row.agent}</AppLink><span>{row.tool}</span></>
      : <><span title={terminal?.warning}>{terminal?.label}</span><span>unattributed</span></>}</td>
    <td className="status-cell">{hasAgent && <AgentStatusDot status={row.bus_status} />}{row.herdr_status} · {row.bus_status !== '-' ? row.bus_status : 'no bus'}{hasAgent && gapLabel(row.gap) && <span className="gap-badge">{gapLabel(row.gap)}</span>}</td>
    <td className="actions-cell">{spawning && hasAgent && <SpawnControl pane={row as Pane} onBanner={onBanner} />}</td>
  </>
}

function RowAndSubagents({ row, spawning = false, onBanner = () => {} }: { row: Row | Pane, spawning?: boolean, onBanner?: (key: string, detail: string) => void }) {
  return <>
    <tr key={`${row.pane_id}:${row.agent}`} className={`${row.agent !== '-' ? 'agent-table-row' : ''}${row.parent_agent ? ' subagent-table-row' : ''}` || undefined} onClick={(event) => {
      if (row.agent !== '-' && !(event.target as HTMLElement).closest('button, a, form')) navigate(`/agents/${encodeURIComponent(row.agent)}`)
    }}><RowCells row={row} spawning={spawning && !row.parent_agent} onBanner={onBanner} /></tr>
    {(row.subagents ?? []).map((child) => <RowAndSubagents key={`${child.parent_agent}:${child.agent}`} row={child} />)}
  </>
}

function Rows({ rows }: { rows: Array<Row | Pane> }) {
  return <div className="table-wrap"><table><thead><tr><th>Pane</th><th>Agent</th><th>Status</th><th>Actions</th></tr></thead><tbody>{rows.map((row) =>
    <RowAndSubagents key={`${row.pane_id}:${row.agent}`} row={row} />)}</tbody></table></div>
}

export function BoardPanel({ board, onBanner }: { board: Board | undefined, onBanner: (key: string, detail: string) => void }) {
  return <main className="panel-scroll board-panel">
    <header className="board-header"><strong>Fleet board</strong><span>{board ? `${board.workspaces.length} workspaces · ${board.workspaces.reduce((count, workspace) => count + workspace.pane_count, 0)} panes` : 'connecting'}</span></header>
    {!board ? <p className="loading">Waiting for the first fleet snapshot…</p> : <>
      <section className="fleet" aria-label="Workspaces">
        {board.workspaces.map((workspace) => <article className="workspace" key={workspace.workspace_id}>
          <div className="section-heading">
            <h2 title={workspace.label || workspace.workspace_id}>{workspaceName(workspace.label, workspace.workspace_id)} {workspace.focused && <span className="focused">focused</span>}</h2>
            <span>{workspace.tab_count} tabs · {workspace.pane_count} panes · {workspace.agent_status || '—'}</span>
          </div>
          <div className="table-wrap"><table><thead><tr><th>Pane</th><th>Agent</th><th>Status</th><th>Actions</th></tr></thead><tbody>
            {workspace.tabs.map((tab) => <Fragment key={tab.tab_id}>
              <tr className="tab-separator"><th colSpan={4}>tab {tab.number}: {tab.label || tab.tab_id} {tab.focused && <span>focused</span>}</th></tr>
              {tab.panes.map((row) => <RowAndSubagents key={`${row.pane_id}:${row.agent}`} row={row} spawning onBanner={onBanner} />)}
            </Fragment>)}
          </tbody></table></div>
        </article>)}
      </section>
      <section className="workspace unplaced">
        <div className="section-heading"><h2>Unplaced</h2><span>{board.unplaced.length} agents</span></div>
        {board.unplaced.length ? <Rows rows={board.unplaced} /> : <p className="empty">No placement gaps.</p>}
      </section>
    </>}
  </main>
}
