import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getGitStatus, queryKeys } from '../../api/client'
import type { FileTarget, GitStatusEntry } from '../../types'
import { Banner } from '../../shared/presentation'
import { rootLabel } from '../files/fileResolution'
import { branchBaseAvailable, branchChangeSummary, changeSideLabel, effectiveChangesBase, entryChangeCount, requestedChangesBase } from './changesModel'
import { repoChangeSummary, type GitBase } from './gitViewModel'
import { openInSideLabel, placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { failureBanner, PanelState, useActivationRefetch } from '../../shared/PanelState'

export function ChangesPanel({ root, active, onOpenDiff }: { root: string, active: boolean, onOpenDiff: (target: FileTarget, base: GitBase, placement?: OpenPlacement) => void }) {
  const [baseChoice, setBaseChoice] = useState<GitBase | null>(null)
  const requestedBase = requestedChangesBase(baseChoice)
  const status = useQuery({ queryKey: queryKeys.gitStatus(root, requestedBase), queryFn: ({ signal }) => getGitStatus(root, fetch, signal, requestedBase), retry: false })
  const failure = status.error ? failureBanner('git status', status.error) : null
  const available = status.data && 'repo' in status.data ? status.data : null
  const branchAvailable = available ? branchBaseAvailable(available.repo.branch_base) : false
  const effectiveBase = effectiveChangesBase(available?.entries_base)
  useActivationRefetch(active, () => { void status.refetch() })
  const commits = available?.repo.branch_base.status === 'available' ? available.repo.branch_base.commits_ahead_of_base : undefined
  return <main className="changes-panel">
    <header className="changes-header">
      <div><strong>Changes</strong><span title={root}>{rootLabel(root)}</span><span className="root-path" title={root}>{root}</span></div>
      <button type="button" onClick={() => status.refetch()} disabled={status.isFetching}>{status.isFetching ? 'Refreshing…' : 'Refresh'}</button>
    </header>
    {failure && <Banner source={failure.source} detail={failure.detail} />}
    {status.isPending && <PanelState as="div" className="file-state">Reading repository changes…</PanelState>}
    {status.data && 'git' in status.data && <PanelState className="file-state" title="Git unavailable" detail={status.data.git.reason} />}
    {available && <>
      <div className="changes-toolbar">
        <label>Changes base <select value={effectiveBase} onChange={(event) => setBaseChoice(event.target.value as GitBase)}>
          <option value="uncommitted">Uncommitted (vs HEAD)</option>
          {branchAvailable && <option value="branch">All work on this branch (vs merge-base with origin/HEAD)</option>}
        </select></label>
        <span>{effectiveBase === 'branch' ? branchChangeSummary(commits, available.entries.length) : repoChangeSummary(commits, available.entries.length)}</span>
        <span>Fetched {new Date(available.fetched_at).toLocaleString()}</span>
      </div>
      {available.entries.length === 0 ? <PanelState className="file-state" title={effectiveBase === 'branch' ? 'No branch changes' : 'Nothing uncommitted'} detail={effectiveBase === 'branch' ? `The working tree matches ${available.entries_base?.label ?? 'the branch merge-base'}.` : 'The working tree matches HEAD.'} />
        : <><h2 className="changes-list-title">{effectiveBase === 'branch' ? 'All work on this branch' : 'Uncommitted files'}</h2><div className="changes-list" role="list">{available.entries.map((entry) => <ChangeRow key={`${entry.path}:${entry.old_path ?? ''}`} entry={entry} base={effectiveBase} onOpen={(placement) => onOpenDiff({ root, path: entry.path }, effectiveBase, placement)} />)}</div></>}
    </>}
  </main>
}

function ChangeRow({ entry, base, onOpen }: { entry: GitStatusEntry, base: GitBase, onOpen: (placement: OpenPlacement) => void }) {
  const sides = changeSideLabel(entry, base)
  return <button type="button" className="change-row" role="listitem" title={`Open diff · ${openInSideLabel(navigator.userAgent)}`} onClick={(event) => onOpen(placementFromModifiers(event))}>
    <span className={`change-kind ${entry.kind}`}>{entry.kind.replace('_', ' ')}</span>
    <span className="change-path">{entry.old_path && <small>{entry.old_path} →</small>}{entry.path}</span>
    {entry.binary && <span className="change-binary">binary</span>}
    {sides && <span className="change-side">{sides}</span>}
    {entryChangeCount(entry) && <span className="change-count">{entryChangeCount(entry)}</span>}
  </button>
}
