import { Fragment } from 'react'
import { AppLink, navigate } from '../../shared/navigation'
import { gapLabel, statusClass, workspaceName } from '../../shared/presentation'
import { SpawnControl } from '../spawn/SpawnControl'
import type { Board, Pane, Row } from '../../types'

function RowCells({ row, spawning = false, onBanner = () => {} }: { row: Row | Pane, spawning?: boolean, onBanner?: (key: string, detail: string) => void }) {
  const hasAgent = row.agent !== '-'
  return <>
    <td className="pane-id">{row.pane_id}</td>
    <td className="agent-cell">{hasAgent
      ? <><AppLink to={`/agents/${encodeURIComponent(row.agent)}`}>{row.agent}</AppLink><span>{row.tool}</span></>
      : <><span>{'label' in row && row.label ? row.label : 'shell'}</span><span>shell</span></>}</td>
    <td className="status-cell"><span className={`status-dot ${statusClass(row.herdr_status)}`} />{row.herdr_status} · {row.bus_status !== '-' ? row.bus_status : 'no bus'}{hasAgent && gapLabel(row.gap) && <span className="gap-badge">{gapLabel(row.gap)}</span>}</td>
    <td className="actions-cell">{spawning && hasAgent && <SpawnControl pane={row as Pane} onBanner={onBanner} />}</td>
  </>
}

function Rows({ rows }: { rows: Array<Row | Pane> }) {
  return <div className="table-wrap"><table><thead><tr><th>Pane</th><th>Agent</th><th>Status</th><th>Actions</th></tr></thead><tbody>{rows.map((row) =>
    <tr key={`${row.pane_id}:${row.agent}`} className={row.agent !== '-' ? 'agent-table-row' : undefined} onClick={(event) => {
      if (row.agent !== '-' && !(event.target as HTMLElement).closest('button, a, form')) navigate(`/agents/${encodeURIComponent(row.agent)}`)
    }}><RowCells row={row} /></tr>)}</tbody></table></div>
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
              {tab.panes.map((row) => <tr key={`${row.pane_id}:${row.agent}`} className={row.agent !== '-' ? 'agent-table-row' : undefined} onClick={(event) => {
                if (row.agent !== '-' && !(event.target as HTMLElement).closest('button, a, form')) navigate(`/agents/${encodeURIComponent(row.agent)}`)
              }}><RowCells row={row} spawning onBanner={onBanner} /></tr>)}
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
