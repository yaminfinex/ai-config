import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiProblem, getGitStatus, queryKeys } from '../../api/client'
import type { FileTarget, GitStatusEntry } from '../../types'
import { Banner } from '../../shared/presentation'
import { rootLabel } from '../files/fileResolution'
import { branchBaseAvailable, entryChangeCount } from './changesModel'
import { repoChangeSummary, type GitBase } from './gitViewModel'

export function ChangesPanel({ root, active, onOpenDiff }: { root: string, active: boolean, onOpenDiff: (target: FileTarget, base: GitBase) => void }) {
  const [base, setBase] = useState<GitBase>('uncommitted')
  const status = useQuery({ queryKey: queryKeys.gitStatus(root), queryFn: ({ signal }) => getGitStatus(root, fetch, signal), retry: false })
  const failure = status.error ? apiProblem(status.error) : null
  const available = status.data && 'repo' in status.data ? status.data : null
  const branchAvailable = available ? branchBaseAvailable(available.repo.branch_base) : false
  const effectiveBase = base === 'branch' && !branchAvailable ? 'uncommitted' : base
  const wasActive = useRef(active)
  useEffect(() => {
    if (active && !wasActive.current) void status.refetch()
    wasActive.current = active
  }, [active])
  const commits = available?.repo.branch_base.status === 'available' ? available.repo.branch_base.commits_ahead_of_base : undefined
  return <main className="changes-panel">
    <header className="changes-header">
      <div><strong>Changes</strong><span title={root}>{rootLabel(root)}</span><span className="root-path" title={root}>{root}</span></div>
      <button type="button" onClick={() => status.refetch()} disabled={status.isFetching}>{status.isFetching ? 'Refreshing…' : 'Refresh'}</button>
    </header>
    {failure && <Banner source="git status" detail={`${failure.problem.error}: ${failure.problem.detail}`} />}
    {status.isPending && <div className="file-state" role="status">Reading repository changes…</div>}
    {status.data && 'git' in status.data && <section className="file-state" role="status"><strong>Git unavailable</strong><p>{status.data.git.reason}</p></section>}
    {available && <>
      <div className="changes-toolbar">
        <label>Diff base for opened files <select value={effectiveBase} onChange={(event) => setBase(event.target.value as GitBase)}>
          <option value="uncommitted">Uncommitted (vs HEAD)</option>
          {branchAvailable && <option value="branch">All work on this branch (vs merge-base with origin/HEAD)</option>}
        </select></label>
        <span>{repoChangeSummary(commits, available.entries.length)}</span>
        <span>Fetched {new Date(available.fetched_at).toLocaleString()}</span>
      </div>
      {available.entries.length === 0 ? <section className="file-state" role="status"><strong>Nothing uncommitted</strong><p>This list shows uncommitted files; committed branch work is available from each file&apos;s History.</p></section>
        : <><h2 className="changes-list-title">Uncommitted files</h2><div className="changes-list" role="list">{available.entries.map((entry) => <ChangeRow key={`${entry.path}:${entry.old_path ?? ''}`} entry={entry} onOpen={() => onOpenDiff({ root, path: entry.path }, effectiveBase)} />)}</div></>}
    </>}
  </main>
}

function ChangeRow({ entry, onOpen }: { entry: GitStatusEntry, onOpen: () => void }) {
  const sides = [entry.staged ? 'staged' : '', entry.unstaged ? 'unstaged' : ''].filter(Boolean).join(' + ')
  return <button type="button" className="change-row" role="listitem" onClick={onOpen}>
    <span className={`change-kind ${entry.kind}`}>{entry.kind.replace('_', ' ')}</span>
    <span className="change-path">{entry.old_path && <small>{entry.old_path} →</small>}{entry.path}</span>
    {entry.binary && <span className="change-binary">binary</span>}
    {sides && <span className="change-side">{sides}</span>}
    {entryChangeCount(entry) && <span className="change-count">{entryChangeCount(entry)}</span>}
  </button>
}
