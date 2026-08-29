import { useEffect, useRef } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { apiProblem, getFile, getGitDiff, getGitFile, getGitLog, getGitStatus, queryKeys, resolveFiles } from '../../api/client'
import type { FileTarget, FolderTarget, GitDiffRead, GitLogEntry, GitLogRead } from '../../types'
import { Banner } from '../../shared/presentation'
import { fileMarkdownComponents, Markdown } from '../../shared/Markdown'
import { FileResults } from './FileResults'
import { fileFailureKind, rootLabel } from './fileResolution'
import { isMarkdownPath, type FileViewMode } from './fileTabs'
import { candidateDestination, missionFacts, missionMarkdownBody } from '../folders/folderModel'
import { PierreFile, PierrePatch } from '../git/PierreView'
import { selectGitFileMode, selectHistoricalDiff, selectHistoricalFile, selectedCurrentLines, type GitBase, type GitFileState } from '../git/gitViewModel'
import { placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { useFileWatch } from '../../stream/fileWatchRegistry'

function formattedBytes(size: number) {
  return `${size.toLocaleString()} bytes`
}

export function FilePanel({ target, viewMode, gitState, active, onViewMode, onGitState, onOpenFile, onOpenFolder }: {
  target: FileTarget
  viewMode: FileViewMode
  gitState: GitFileState
  active: boolean
  onViewMode: (mode: FileViewMode) => void
  onGitState: (state: GitFileState) => void
  onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void
  onOpenFolder: (target: FolderTarget, placement?: OpenPlacement) => void
}) {
  useFileWatch({ kind: 'file', root: target.root, path: target.path }, gitState.mode === 'current' && !gitState.revision)
  const fileQuery = useQuery({
    queryKey: queryKeys.file(target.root, target.path),
    queryFn: ({ signal }) => getFile(target.root, target.path, fetch, signal),
    enabled: gitState.mode === 'current' && !gitState.revision,
    retry: false,
  })
  const revisionQuery = useQuery({
    queryKey: gitState.revision ? queryKeys.gitFile(target.root, gitState.revision.path, gitState.revision.sha) : ['git-file', 'inactive'],
    queryFn: ({ signal }) => getGitFile(target.root, gitState.revision!.path, gitState.revision!.sha, fetch, signal),
    enabled: gitState.mode === 'current' && Boolean(gitState.revision),
    staleTime: Infinity,
    retry: false,
  })
  const statusQuery = useQuery({
    queryKey: queryKeys.gitStatus(target.root),
    queryFn: ({ signal }) => getGitStatus(target.root, fetch, signal),
    retry: false,
  })
  const gitAvailable = Boolean(statusQuery.data && 'repo' in statusQuery.data)
  const branchAvailable = Boolean(statusQuery.data && 'repo' in statusQuery.data && statusQuery.data.repo.branch_base.status === 'available')
  const effectiveBase: GitBase = gitState.base === 'branch' && !branchAvailable ? 'uncommitted' : gitState.base
  const diffPath = gitState.commit?.path ?? target.path
  const diffBase = gitState.commit ? 'commit' : effectiveBase
  const diffQuery = useQuery({
    queryKey: queryKeys.gitDiff(target.root, diffPath, diffBase, gitState.commit?.sha),
    queryFn: ({ signal }) => getGitDiff(target.root, diffPath, diffBase, fetch, signal, gitState.commit?.sha),
    enabled: gitState.mode === 'diff' && gitAvailable,
    staleTime: gitState.commit ? Infinity : 0,
    retry: false,
  })
  const historyQuery = useQuery({
    queryKey: queryKeys.gitLog(target.root, target.path),
    queryFn: ({ signal }) => getGitLog(target.root, target.path, undefined, fetch, signal),
    enabled: gitState.mode === 'history' && gitAvailable,
    retry: false,
  })
  const currentError = gitState.revision ? revisionQuery.error : fileQuery.error
  const failure = currentError ? apiProblem(currentError) : null
  const failureKind = fileFailureKind(failure?.response?.status, failure?.problem.error)
  const vanished = failureKind === 'vanished'
  const unknownRoot = failureKind === 'unknown-root'
  const needsAlternatives = !gitState.revision && (vanished || unknownRoot)
  const alternatives = useQuery({
    queryKey: queryKeys.resolve(target.path),
    queryFn: ({ signal }) => resolveFiles(target.path, undefined, fetch, signal),
    enabled: needsAlternatives,
    retry: false,
  })
  const wasActive = useRef(active)

  useEffect(() => {
    if (active && !wasActive.current) {
      if (gitState.mode === 'current' && !gitState.revision) void fileQuery.refetch()
      void statusQuery.refetch()
      if (gitState.mode === 'diff' && gitAvailable) void diffQuery.refetch()
      if (gitState.mode === 'history' && gitAvailable) void historyQuery.refetch()
    }
    wasActive.current = active
  }, [active])

  useEffect(() => {
    if (gitState.base === 'branch' && statusQuery.data && !branchAvailable) onGitState({ ...gitState, base: 'uncommitted' })
  }, [branchAvailable, gitState, onGitState, statusQuery.data])

  const data = gitState.revision ? revisionQuery.data : fileQuery.data
  const viewedPath = gitState.revision?.path ?? target.path
  const markdown = isMarkdownPath(viewedPath)
  const missionMarkdown = Boolean(data && !data.binary && /(?:^|\/)mission\.md$/iu.test(viewedPath))
  const facts = data && !data.binary && missionMarkdown ? missionFacts(data.content) : null
  const hasFacts = facts && Object.keys(facts).length > 0
  const gitReason = statusQuery.isPending ? 'Verifying Git availability…'
    : statusQuery.error ? `Git status unavailable: ${statusQuery.error.message}`
      : statusQuery.data && 'git' in statusQuery.data ? statusQuery.data.git.reason : ''
  const refresh = () => {
    void statusQuery.refetch()
    if (gitState.mode === 'current' && !gitState.revision) void fileQuery.refetch()
    else if (gitState.mode === 'diff' && gitAvailable) void diffQuery.refetch()
    else if (gitState.mode === 'history' && gitAvailable) void historyQuery.refetch()
  }
  const refreshing = statusQuery.isFetching || gitState.mode === 'current' && !gitState.revision && fileQuery.isFetching || gitState.mode === 'diff' && diffQuery.isFetching || gitState.mode === 'history' && historyQuery.isFetching
  return <main className="file-panel">
    <header className="file-header">
      <div className="file-title"><strong>{rootLabel(target.path)}</strong><span>{target.path}</span><span className="root-path" title={target.root}>{target.root}</span></div>
      <div className="detail-toggle file-mode-toggle" aria-label="File mode">
        {(['current', 'diff', 'history'] as const).map((mode) => <button type="button" key={mode} className={gitState.mode === mode ? 'active' : ''} aria-pressed={gitState.mode === mode}
          disabled={mode !== 'current' && !gitAvailable} title={mode !== 'current' && gitReason ? gitReason : undefined}
          onClick={() => onGitState(selectGitFileMode(gitState, mode))}>{mode[0].toUpperCase() + mode.slice(1)}</button>)}
      </div>
      {gitState.mode === 'current' && markdown && <div className="detail-toggle file-view-toggle" aria-label="Markdown view">
        <button type="button" className={viewMode === 'rendered' ? 'active' : ''} aria-pressed={viewMode === 'rendered'} onClick={() => onViewMode('rendered')}>Rendered</button>
        <button type="button" className={viewMode === 'source' ? 'active' : ''} aria-pressed={viewMode === 'source'} onClick={() => onViewMode('source')}>Source</button>
      </div>}
      <button type="button" onClick={refresh} disabled={refreshing}>{refreshing ? 'Refreshing…' : 'Refresh'}</button>
    </header>
    {gitState.mode === 'current' && (gitState.revision ? revisionQuery.isPending : fileQuery.isPending) && <div className="file-state" role="status">Reading {gitState.revision ? 'historical revision' : 'current file'}…</div>}
    {gitState.mode === 'current' && failure && !needsAlternatives && <Banner source={gitState.revision ? 'git file' : 'file'} detail={`${failure.problem.error}: ${failure.problem.detail}`} />}
    {gitState.mode === 'current' && needsAlternatives && <section className="file-state vanished" role="status"><strong>{vanished ? 'File vanished' : 'Root no longer served'}</strong><p>{vanished ? 'This path no longer exists in its root.' : 'This file root is no longer in the live readable universe.'} Closest current matches:</p>
      {alternatives.isPending && <p>Resolving current files…</p>}
      {alternatives.error && <Banner source="resolve" detail={alternatives.error.message} />}
      {!alternatives.isFetching && !alternatives.error && <FileResults resolution={alternatives.data} limit={8} onSelect={(candidate, event) => {
        const placement = placementFromModifiers(event)
        if (candidateDestination(candidate) === 'folder') onOpenFolder({ root: candidate.root, path: candidate.path }, placement)
        else onOpenFile({ root: candidate.root, path: candidate.path, line: target.line }, placement)
      }} />}
    </section>}
    {gitState.mode === 'current' && data && !failure && <>
      <div className="file-facts">{'fetched_at' in data ? <span>Fetched {new Date(data.fetched_at).toLocaleString()}</span> : <span>Revision {data.sha.slice(0, 12)} · immutable</span>}<span>{formattedBytes(data.size)}</span>{gitState.revision && <span>{gitState.revision.path}</span>}{target.line && !gitState.revision && <span>line {target.line}</span>}</div>
      {markdown && viewMode === 'rendered' && hasFacts && <section className="mission-fact-strip" aria-label="Mission facts">
        {facts.title && <span><small>title</small>{facts.title}</span>}
        {facts.status && <span><small>status</small>{facts.status}</span>}
        {facts.created && <span><small>created</small>{facts.created}</span>}
        {facts.updated && <span><small>updated</small>{facts.updated}</span>}
      </section>}
      {data.binary ? <section className="file-state binary" role="status"><strong>Binary file</strong><p>No text content is available for this {formattedBytes(data.size)} file.</p></section>
        : <div className="file-content" role="region" aria-label={`Read-only contents of ${data.path}`}>
          {data.truncated && <div className="truncation-banner">Showing the first 256 KiB of {formattedBytes(data.size)}. The file is truncated.</div>}
          {markdown && viewMode === 'rendered' ? <div className="markdown file-markdown"><Markdown components={fileMarkdownComponents}>{missionMarkdown ? missionMarkdownBody(data.content) : data.content}</Markdown></div>
            : <div className="file-source"><PierreFile path={gitState.revision?.path ?? data.path} content={data.content} selectedLines={gitState.revision ? null : selectedCurrentLines(target.line)} /></div>
          }
        </div>}
    </>}
    {gitState.mode !== 'current' && !gitAvailable && <section className="file-state" role="status"><strong>Git unavailable</strong><p>{gitReason || 'This root cannot currently provide Git facts.'}</p></section>}
    {gitState.mode === 'diff' && gitAvailable && <DiffView query={diffQuery} base={effectiveBase} commit={gitState.commit?.sha} branchAvailable={branchAvailable} onBase={(base) => onGitState({ mode: 'diff', base })} />}
    {gitState.mode === 'history' && gitAvailable && <HistoryView page={historyQuery.data} pending={historyQuery.isPending} error={historyQuery.error}
      onFile={(entry) => onGitState(selectHistoricalFile(gitState, { sha: entry.sha, path: entry.path_then }))}
      onDiff={(entry) => onGitState(selectHistoricalDiff(gitState, { sha: entry.sha, path: entry.path_then }))} />}
  </main>
}

function DiffView({ query, base, commit, branchAvailable, onBase }: {
  query: UseQueryResult<GitDiffRead, Error>
  base: GitBase
  commit?: string
  branchAvailable: boolean
  onBase: (base: GitBase) => void
}) {
  const failure = query.error ? apiProblem(query.error) : null
  const data = query.data
  return <section className="git-mode-body" aria-label="Read-only Git diff">
    <div className="git-base-bar">{commit ? <><span>What commit {commit.slice(0, 12)} changed</span><button type="button" onClick={() => onBase(base)}>Return to working diff</button></> : <label>Base <select value={base} onChange={(event) => onBase(event.target.value as GitBase)}>
      <option value="uncommitted">Uncommitted (vs HEAD)</option>
      {branchAvailable && <option value="branch">All work on this branch (vs merge-base with origin/HEAD)</option>}
    </select></label>}</div>
    {query.isPending && <div className="file-state" role="status">Reading diff…</div>}
    {failure && <Banner source="git diff" detail={`${failure.problem.error}: ${failure.problem.detail}`} />}
    {data && <>
      <div className="file-facts"><span>Fetched {data.fetched_at ? new Date(data.fetched_at).toLocaleString() : 'immutable revision'}</span><span>{data.base.label}</span>{data.stats && <span>+{data.stats.additions} / −{data.stats.deletions}</span>}</div>
      <DiffFacts data={data} />
      {data.facts.binary ? <section className="file-state binary" role="status"><strong>Binary change</strong><p>Git reports this file as binary; no text patch is available.</p></section>
        : data.patch.length === 0 ? <section className="file-state" role="status"><strong>No changes vs this base</strong><p>The selected file has no patch against {data.base.label}.</p></section>
          : <div className="git-diff-content"><PierrePatch patch={data.patch} /></div>}
    </>}
  </section>
}

function HistoryView({ page, pending, error, onFile, onDiff }: {
  page?: GitLogRead
  pending: boolean
  error: Error | null
  onFile: (entry: GitLogEntry) => void
  onDiff: (entry: GitLogEntry) => void
}) {
  const entries = page?.entries ?? []
  const failure = error ? apiProblem(error) : null
  return <section className="git-mode-body" aria-label="File history">
    {pending && <div className="file-state" role="status">Reading file history…</div>}
    {failure && <Banner source="git log" detail={`${failure.problem.error}: ${failure.problem.detail}`} />}
    {!pending && !failure && entries.length === 0 && <div className="file-state" role="status"><strong>No history</strong><p>Git has no commits for this path.</p></div>}
    {entries.length > 0 && <div className="history-list" role="list">{entries.map((entry) => <article className="history-row" role="listitem" key={entry.sha}>
      <div className="history-subject"><strong>{entry.subject}</strong><span>{entry.sha.slice(0, 12)}</span></div>
      <div className="history-meta"><span>{entry.author}</span><time dateTime={entry.date}>{new Date(entry.date).toLocaleString()}</time><span title={entry.path_then}>{entry.path_then}</span></div>
      <div className="history-actions"><button type="button" onClick={() => onFile(entry)}>View file at commit</button><button type="button" onClick={() => onDiff(entry)}>What this commit changed</button></div>
    </article>)}</div>}
    {page?.next_cursor && <p className="history-more" role="note">Showing the 50 most recent commits; older history is not yet available.</p>}
  </section>
}

function DiffFacts({ data }: { data: Awaited<ReturnType<typeof getGitDiff>> }) {
  const facts = []
  if (data.truncated) facts.push(`Showing the first 256 KiB of a ${data.patch_bytes.toLocaleString()} byte patch. The diff is truncated.`)
  if (data.facts.old_path) facts.push(`Renamed from ${data.facts.old_path}.`)
  if (data.facts.old_mode && data.facts.new_mode) facts.push(`Mode changed ${data.facts.old_mode} → ${data.facts.new_mode}.`)
  if (facts.length === 0) return null
  return <div className="git-fact-banners">{facts.map((fact) => <div className="truncation-banner" key={fact}>{fact}</div>)}</div>
}
