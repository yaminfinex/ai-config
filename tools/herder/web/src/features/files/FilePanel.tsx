import { useEffect, useRef } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { apiProblem, getFile, getGitDiff, getGitStatus, queryKeys, resolveFiles } from '../../api/client'
import type { FileTarget, FolderTarget, GitDiffRead } from '../../types'
import { Banner } from '../../shared/presentation'
import { fileMarkdownComponents, Markdown } from '../../shared/Markdown'
import { FileResults } from './FileResults'
import { fileFailureKind, rootLabel } from './fileResolution'
import { isMarkdownPath, type FileViewMode } from './fileTabs'
import { candidateDestination, missionFacts, missionMarkdownBody } from '../folders/folderModel'
import { PierreFile, PierrePatch } from '../git/PierreView'
import { selectedCurrentLines, type GitBase, type GitFileState } from '../git/gitViewModel'

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
  onOpenFile: (target: FileTarget) => void
  onOpenFolder: (target: FolderTarget) => void
}) {
  const fileQuery = useQuery({
    queryKey: queryKeys.file(target.root, target.path),
    queryFn: ({ signal }) => getFile(target.root, target.path, fetch, signal),
    enabled: gitState.mode === 'current',
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
  const diffQuery = useQuery({
    queryKey: queryKeys.gitDiff(target.root, target.path, effectiveBase),
    queryFn: ({ signal }) => getGitDiff(target.root, target.path, effectiveBase, fetch, signal),
    enabled: gitState.mode === 'diff' && gitAvailable,
    retry: false,
  })
  const failure = fileQuery.error ? apiProblem(fileQuery.error) : null
  const failureKind = fileFailureKind(failure?.response?.status, failure?.problem.error)
  const vanished = failureKind === 'vanished'
  const unknownRoot = failureKind === 'unknown-root'
  const needsAlternatives = vanished || unknownRoot
  const alternatives = useQuery({
    queryKey: queryKeys.resolve(target.path),
    queryFn: ({ signal }) => resolveFiles(target.path, undefined, fetch, signal),
    enabled: needsAlternatives,
    retry: false,
  })
  const wasActive = useRef(active)

  useEffect(() => {
    if (active && !wasActive.current) {
      if (gitState.mode === 'current') void fileQuery.refetch()
      void statusQuery.refetch()
      if (gitState.mode === 'diff' && gitAvailable) void diffQuery.refetch()
    }
    wasActive.current = active
  }, [active])

  useEffect(() => {
    if (gitState.base === 'branch' && statusQuery.data && !branchAvailable) onGitState({ ...gitState, base: 'uncommitted' })
  }, [branchAvailable, gitState, onGitState, statusQuery.data])

  const data = fileQuery.data
  const markdown = isMarkdownPath(target.path)
  const missionMarkdown = Boolean(data && !data.binary && /(?:^|\/)mission\.md$/iu.test(target.path))
  const facts = data && !data.binary && missionMarkdown ? missionFacts(data.content) : null
  const hasFacts = facts && Object.keys(facts).length > 0
  const gitReason = statusQuery.isPending ? 'Verifying Git availability…'
    : statusQuery.error ? `Git status unavailable: ${statusQuery.error.message}`
      : statusQuery.data && 'git' in statusQuery.data ? statusQuery.data.git.reason : ''
  const refresh = () => {
    void statusQuery.refetch()
    if (gitState.mode === 'current') void fileQuery.refetch()
    else if (gitState.mode === 'diff' && gitAvailable) void diffQuery.refetch()
  }
  const refreshing = statusQuery.isFetching || gitState.mode === 'current' && fileQuery.isFetching || gitState.mode === 'diff' && diffQuery.isFetching
  return <main className="file-panel">
    <header className="file-header">
      <div className="file-title"><strong>{rootLabel(target.path)}</strong><span>{target.path}</span><span className="root-path" title={target.root}>{target.root}</span></div>
      <div className="detail-toggle file-mode-toggle" aria-label="File mode">
        {(['current', 'diff', 'history'] as const).map((mode) => <button type="button" key={mode} className={gitState.mode === mode ? 'active' : ''} aria-pressed={gitState.mode === mode}
          disabled={mode !== 'current' && !gitAvailable} title={mode !== 'current' && gitReason ? gitReason : undefined}
          onClick={() => onGitState({ ...gitState, mode })}>{mode[0].toUpperCase() + mode.slice(1)}</button>)}
      </div>
      {gitState.mode === 'current' && markdown && <div className="detail-toggle file-view-toggle" aria-label="Markdown view">
        <button type="button" className={viewMode === 'rendered' ? 'active' : ''} aria-pressed={viewMode === 'rendered'} onClick={() => onViewMode('rendered')}>Rendered</button>
        <button type="button" className={viewMode === 'source' ? 'active' : ''} aria-pressed={viewMode === 'source'} onClick={() => onViewMode('source')}>Source</button>
      </div>}
      <button type="button" onClick={refresh} disabled={refreshing}>{refreshing ? 'Refreshing…' : 'Refresh'}</button>
    </header>
    {gitState.mode === 'current' && fileQuery.isPending && <div className="file-state" role="status">Reading current file…</div>}
    {gitState.mode === 'current' && failure && !needsAlternatives && <Banner source="file" detail={`${failure.problem.error}: ${failure.problem.detail}`} />}
    {gitState.mode === 'current' && needsAlternatives && <section className="file-state vanished" role="status"><strong>{vanished ? 'File vanished' : 'Root no longer served'}</strong><p>{vanished ? 'This path no longer exists in its root.' : 'This file root is no longer in the live readable universe.'} Closest current matches:</p>
      {alternatives.isPending && <p>Resolving current files…</p>}
      {alternatives.error && <Banner source="resolve" detail={alternatives.error.message} />}
      {!alternatives.isFetching && !alternatives.error && <FileResults resolution={alternatives.data} limit={8} onSelect={(candidate) => {
        if (candidateDestination(candidate) === 'folder') onOpenFolder({ root: candidate.root, path: candidate.path })
        else onOpenFile({ root: candidate.root, path: candidate.path, line: target.line })
      }} />}
    </section>}
    {gitState.mode === 'current' && data && !failure && <>
      <div className="file-facts"><span>Fetched {new Date(data.fetched_at).toLocaleString()}</span><span>{formattedBytes(data.size)}</span>{target.line && <span>line {target.line}</span>}</div>
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
            : <div className="file-source"><PierreFile path={data.path} content={data.content} selectedLines={selectedCurrentLines(target.line)} /></div>
          }
        </div>}
    </>}
    {gitState.mode === 'diff' && <DiffView query={diffQuery} base={effectiveBase} branchAvailable={branchAvailable} onBase={(base) => onGitState({ ...gitState, base })} />}
    {gitState.mode === 'history' && <section className="file-state" role="status"><strong>Loading history support…</strong><p>Commit history will appear here when the server&apos;s revision-facts addendum is available.</p></section>}
  </main>
}

function DiffView({ query, base, branchAvailable, onBase }: {
  query: UseQueryResult<GitDiffRead, Error>
  base: GitBase
  branchAvailable: boolean
  onBase: (base: GitBase) => void
}) {
  const failure = query.error ? apiProblem(query.error) : null
  const data = query.data
  return <section className="git-mode-body" aria-label="Read-only Git diff">
    <div className="git-base-bar"><label>Base <select value={base} onChange={(event) => onBase(event.target.value as GitBase)}>
      <option value="uncommitted">Uncommitted (vs HEAD)</option>
      {branchAvailable && <option value="branch">All work on this branch (vs merge-base with origin/HEAD)</option>}
    </select></label></div>
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

function DiffFacts({ data }: { data: Awaited<ReturnType<typeof getGitDiff>> }) {
  const facts = []
  if (data.truncated) facts.push(`Showing the first 256 KiB of a ${data.patch_bytes.toLocaleString()} byte patch. The diff is truncated.`)
  if (data.facts.old_path) facts.push(`Renamed from ${data.facts.old_path}.`)
  if (data.facts.old_mode && data.facts.new_mode) facts.push(`Mode changed ${data.facts.old_mode} → ${data.facts.new_mode}.`)
  if (facts.length === 0) return null
  return <div className="git-fact-banners">{facts.map((fact) => <div className="truncation-banner" key={fact}>{fact}</div>)}</div>
}
