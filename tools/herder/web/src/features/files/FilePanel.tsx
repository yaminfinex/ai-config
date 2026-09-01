import { useEffect, useMemo } from 'react'
import { useInfiniteQuery, useQuery, type UseQueryResult } from '@tanstack/react-query'
import { getFile, getGitDiff, getGitFile, getGitLog, getGitStatus, queryKeys, resolveFiles } from '../../api/client'
import type { FileTarget, FolderTarget, GitDiffRead, GitLogEntry, GitLogRead } from '../../types'
import { Banner } from '../../shared/presentation'
import { fileMarkdownComponents, Markdown } from '../../shared/Markdown'
import { FileResults } from './FileResults'
import { fileFailureKind, rootLabel } from './fileResolution'
import { isHtmlPath, isMarkdownPath, type FileViewMode } from './fileTabs'
import { candidateDestination, missionFacts, missionMarkdownBody, parentFolderPath, rootJoinedAbsolutePath } from '../folders/folderModel'
import { PierreFile, PierrePatch } from '../git/PierreView'
import { selectGitFileMode, selectHistoricalDiff, selectHistoricalFile, selectedCurrentLines, type GitBase, type GitFileState } from '../git/gitViewModel'
import { openInSideLabel, placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { useFileWatch } from '../../stream/fileWatchRegistry'
import { failureBanner, PanelState, useActivationRefetch } from '../../shared/PanelState'
import { historyPagingState } from './historyPaging'
import { PathCopyButton } from '../../shared/PathCopyButton'
import { useTranscriptFileResolver } from './TranscriptFileResolver'
import { useNoteCapture } from '../notes/useNoteCapture'
import type { NoteSource } from '../notes/notesStore'

function formattedBytes(size: number) {
  return `${size.toLocaleString()} bytes`
}

export function FilePanel({ target, agents, viewMode, gitState, active, onViewMode, onGitState, onOpenFile, onOpenFolder }: {
  target: FileTarget
  agents: string[]
  viewMode: FileViewMode
  gitState: GitFileState
  active: boolean
  onViewMode: (mode: FileViewMode) => void
  onGitState: (state: GitFileState) => void
  onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void
  onOpenFolder: (target: FolderTarget, placement?: OpenPlacement, selectionHint?: FileTarget) => void
}) {
  const viewedPath = gitState.revision?.path ?? target.path
  const resolverContext = useMemo(() => ({ root: target.root, path: viewedPath }), [target.root, viewedPath])
  const fileResolver = useTranscriptFileResolver(resolverContext, active && gitState.mode === 'current', onOpenFile, onOpenFolder)
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
  const captureSource: NoteSource = gitState.mode === 'diff'
    ? { kind: 'diff', path: diffPath, base: diffQuery.data?.base.label ?? (gitState.commit ? `commit ${gitState.commit.sha.slice(0, 12)}` : effectiveBase === 'branch' ? 'merge-base' : 'HEAD') }
    : { kind: 'file', path: viewedPath }
  const noteCapture = useNoteCapture({ active: active && gitState.mode !== 'history', source: captureSource, agents })
  const historyQuery = useInfiniteQuery({
    queryKey: queryKeys.gitLog(target.root, target.path),
    queryFn: ({ pageParam, signal }) => getGitLog(target.root, target.path, pageParam ?? undefined, fetch, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: gitState.mode === 'history' && gitAvailable,
    retry: false,
  })
  const currentError = gitState.revision ? revisionQuery.error : fileQuery.error
  const failure = currentError ? failureBanner(gitState.revision ? 'git file' : 'file', currentError) : null
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
  useActivationRefetch(active, () => {
      if (gitState.mode === 'current' && !gitState.revision) void fileQuery.refetch()
      void statusQuery.refetch()
      if (gitState.mode === 'diff' && gitAvailable) void diffQuery.refetch()
      if (gitState.mode === 'history' && gitAvailable) void historyQuery.refetch()
  })

  useEffect(() => {
    if (gitState.base === 'branch' && statusQuery.data && !branchAvailable) onGitState({ ...gitState, base: 'uncommitted' })
  }, [branchAvailable, gitState, onGitState, statusQuery.data])

  const data = gitState.revision ? revisionQuery.data : fileQuery.data
  const markdown = isMarkdownPath(viewedPath)
  const html = isHtmlPath(viewedPath)
  const renderable = markdown || html
  const truncated = Boolean(data && !data.binary && data.truncated)
  const effectiveViewMode = html && truncated ? 'source' : viewMode
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
  const containingFolder = parentFolderPath(target.path) ?? ''
  const absolutePath = rootJoinedAbsolutePath(target.root, target.path)
  return <main className="file-panel" ref={noteCapture.containerRef} onPointerUp={noteCapture.onPointerUp}>
    <header className="file-header">
      <div className="file-title"><div className="path-name"><strong>{rootLabel(target.path)}</strong><PathCopyButton key={absolutePath} path={absolutePath} /><button type="button" className={`header-refresh${refreshing ? ' busy' : ''}`}
        title={refreshing ? 'Refreshing…' : 'Refresh'} aria-label="Refresh" onClick={refresh} disabled={refreshing}>
        <svg viewBox="0 0 14 14" aria-hidden="true" focusable="false"><path d="M12 7a5 5 0 1 1-1.46-3.54" /><path d="M12.5 1.5v2.6h-2.6" /></svg>
      </button></div><a className="path-parent-link" href={`/folder?${new URLSearchParams({ root: target.root, path: containingFolder })}`}
        title={`Open ${rootJoinedAbsolutePath(target.root, containingFolder)} · ${openInSideLabel(navigator.userAgent)}`} onClick={(event) => {
          event.preventDefault()
          onOpenFolder({ root: target.root, path: containingFolder }, placementFromModifiers(event), target)
        }}>{containingFolder || '.'}</a><span className="root-path" title={target.root}>{target.root}</span></div>
      <div className="detail-toggle file-mode-toggle" aria-label="File mode">
        {(['current', 'diff', 'history'] as const).map((mode) => <button type="button" key={mode} className={gitState.mode === mode ? 'active' : ''} aria-pressed={gitState.mode === mode}
          disabled={mode !== 'current' && !gitAvailable} title={mode !== 'current' && gitReason ? gitReason : undefined}
          onClick={() => onGitState(selectGitFileMode(gitState, mode))}>{mode[0].toUpperCase() + mode.slice(1)}</button>)}
      </div>
      {gitState.mode === 'current' && renderable && <div className="detail-toggle file-view-toggle" aria-label={`${html ? 'HTML' : 'Markdown'} view`}>
        <button type="button" className={effectiveViewMode === 'rendered' ? 'active' : ''} aria-pressed={effectiveViewMode === 'rendered'} disabled={html && truncated}
          title={html ? truncated ? 'Rendered view is unavailable because this file is truncated.' : 'Render HTML. Scripts do not run.' : undefined} onClick={() => onViewMode('rendered')}>Rendered</button>
        <button type="button" className={effectiveViewMode === 'source' ? 'active' : ''} aria-pressed={effectiveViewMode === 'source'} onClick={() => onViewMode('source')}>Source</button>
      </div>}
    </header>
    {gitState.mode === 'current' && (gitState.revision ? revisionQuery.isPending : fileQuery.isPending) && <PanelState as="div" className="file-state">Reading {gitState.revision ? 'historical revision' : 'current file'}…</PanelState>}
    {gitState.mode === 'current' && failure && !needsAlternatives && <Banner source={failure.source} detail={failure.detail} />}
    {gitState.mode === 'current' && needsAlternatives && <PanelState className="file-state vanished" title={vanished ? 'File vanished' : 'Root no longer served'} detail={<>{vanished ? 'This path no longer exists in its root.' : 'This file root is no longer in the live readable universe.'} Closest current matches:</>}>
      {alternatives.isPending && <p>Resolving current files…</p>}
      {alternatives.error && <Banner source="resolve" detail={alternatives.error.message} />}
      {!alternatives.isFetching && !alternatives.error && <FileResults resolution={alternatives.data} limit={8} onSelect={(candidate, event) => {
        const placement = placementFromModifiers(event)
        if (candidateDestination(candidate) === 'folder') onOpenFolder({ root: candidate.root, path: candidate.path }, placement)
        else onOpenFile({ root: candidate.root, path: candidate.path, line: target.line }, placement)
      }} />}
    </PanelState>}
    {gitState.mode === 'current' && data && !failure && <>
      <div className="file-facts">{'fetched_at' in data ? <span>Fetched {new Date(data.fetched_at).toLocaleString()}</span> : <span>Revision {data.sha.slice(0, 12)} · immutable</span>}<span>{formattedBytes(data.size)}</span>{gitState.revision && <span>{gitState.revision.path}</span>}{target.line && !gitState.revision && <span>line {target.line}</span>}</div>
      {markdown && viewMode === 'rendered' && hasFacts && <section className="mission-fact-strip" aria-label="Mission facts">
        {facts.title && <span><small>title</small>{facts.title}</span>}
        {facts.status && <span><small>status</small>{facts.status}</span>}
        {facts.created && <span><small>created</small>{facts.created}</span>}
        {facts.updated && <span><small>updated</small>{facts.updated}</span>}
      </section>}
      {data.binary ? <PanelState className="file-state binary" title="Binary file" detail={<>No text content is available for this {formattedBytes(data.size)} file.</>} />
        : <div className="file-content" role="region" aria-label={`Read-only contents of ${data.path}`} onDoubleClick={fileResolver.onDoubleClick}>
          {data.truncated && <div className="truncation-banner">Showing the first 256 KiB of {formattedBytes(data.size)}. The file is truncated.</div>}
          {html && effectiveViewMode === 'rendered' ? <iframe className="file-html-preview" title="Rendered HTML preview. Scripts do not run." sandbox="" srcDoc={data.content} />
            : markdown && effectiveViewMode === 'rendered' ? <div className="markdown file-markdown"><Markdown components={fileMarkdownComponents}>{missionMarkdown ? missionMarkdownBody(data.content) : data.content}</Markdown></div>
            : <div className="file-source"><PierreFile path={gitState.revision?.path ?? data.path} content={data.content} selectedLines={gitState.revision ? null : selectedCurrentLines(target.line)} /></div>
          }
        </div>}
    </>}
    {gitState.mode !== 'current' && !gitAvailable && <PanelState className="file-state" title="Git unavailable" detail={gitReason || 'This root cannot currently provide Git facts.'} />}
    {gitState.mode === 'diff' && gitAvailable && <DiffView query={diffQuery} base={effectiveBase} commit={gitState.commit?.sha} branchAvailable={branchAvailable} onBase={(base) => onGitState({ mode: 'diff', base })} />}
    {gitState.mode === 'history' && gitAvailable && <HistoryView pages={historyQuery.data?.pages} pending={historyQuery.isPending}
      initialError={historyQuery.data ? null : historyQuery.error} olderError={historyQuery.isFetchNextPageError ? historyQuery.error : null}
      loadingOlder={historyQuery.isFetchingNextPage} onLoadOlder={() => { void historyQuery.fetchNextPage() }}
      onFile={(entry) => onGitState(selectHistoricalFile(gitState, { sha: entry.sha, path: entry.path_then }))}
      onDiff={(entry) => onGitState(selectHistoricalDiff(gitState, { sha: entry.sha, path: entry.path_then }))} />}
    {fileResolver.element}
    {noteCapture.element}
  </main>
}

function DiffView({ query, base, commit, branchAvailable, onBase }: {
  query: UseQueryResult<GitDiffRead, Error>
  base: GitBase
  commit?: string
  branchAvailable: boolean
  onBase: (base: GitBase) => void
}) {
  const failure = query.error ? failureBanner('git diff', query.error) : null
  const data = query.data
  return <section className="git-mode-body" aria-label="Read-only Git diff">
    <div className="git-base-bar">{commit ? <><span>What commit {commit.slice(0, 12)} changed</span><button type="button" onClick={() => onBase(base)}>Return to working diff</button></> : <label>Base <select value={base} onChange={(event) => onBase(event.target.value as GitBase)}>
      <option value="uncommitted">Uncommitted (vs HEAD)</option>
      {branchAvailable && <option value="branch">All work on this branch (vs merge-base with origin/HEAD)</option>}
    </select></label>}</div>
    {query.isPending && <PanelState as="div" className="file-state">Reading diff…</PanelState>}
    {failure && <Banner source={failure.source} detail={failure.detail} />}
    {data && <>
      <div className="file-facts"><span>Fetched {data.fetched_at ? new Date(data.fetched_at).toLocaleString() : 'immutable revision'}</span><span>{data.base.label}</span>{data.stats && <span>+{data.stats.additions} / −{data.stats.deletions}</span>}</div>
      <DiffFacts data={data} />
      {data.facts.binary ? <PanelState className="file-state binary" title="Binary change" detail="Git reports this file as binary; no text patch is available." />
        : data.patch.length === 0 ? <PanelState className="file-state" title="No changes vs this base" detail={<>The selected file has no patch against {data.base.label}.</>} />
          : <div className="git-diff-content"><PierrePatch patch={data.patch} /></div>}
    </>}
  </section>
}

function HistoryView({ pages, pending, initialError, olderError, loadingOlder, onLoadOlder, onFile, onDiff }: {
  pages?: GitLogRead[]
  pending: boolean
  initialError: Error | null
  olderError: Error | null
  loadingOlder: boolean
  onLoadOlder: () => void
  onFile: (entry: GitLogEntry) => void
  onDiff: (entry: GitLogEntry) => void
}) {
  const history = historyPagingState(pages)
  const failure = initialError ? failureBanner('git log', initialError) : null
  return <section className="git-mode-body" aria-label="File history">
    {pending && <PanelState as="div" className="file-state">Reading file history…</PanelState>}
    {failure && <Banner source={failure.source} detail={failure.detail} />}
    {!pending && !failure && history.entries.length === 0 && <PanelState as="div" className="file-state" title="No history" detail="Git has no commits for this path." />}
    {history.entries.length > 0 && <div className="history-list" role="list">{history.entries.map((entry) => <article className="history-row" role="listitem" key={entry.sha}>
      <div className="history-subject"><strong>{entry.subject}</strong><span>{entry.sha.slice(0, 12)}</span></div>
      <div className="history-meta"><span>{entry.author}</span><time dateTime={entry.date}>{new Date(entry.date).toLocaleString()}</time><span title={entry.path_then}>{entry.path_then}</span></div>
      <div className="history-actions"><button type="button" onClick={() => onFile(entry)}>View file at commit</button><button type="button" onClick={() => onDiff(entry)}>What this commit changed</button></div>
    </article>)}</div>}
    {history.entries.length > 0 && history.end === 'more' && !loadingOlder && !olderError && <button type="button" className="history-more" onClick={onLoadOlder}>Load 50 older commits</button>}
    {loadingOlder && <PanelState as="div" className="history-more history-more-state">Loading older commits…</PanelState>}
    {olderError && <PanelState as="div" className="history-more history-more-state" role="alert" title="Older commits could not be loaded" detail={olderError.message}>
      <button type="button" onClick={onLoadOlder}>Try again</button>
    </PanelState>}
    {history.entries.length > 0 && history.end === 'beginning' && <PanelState as="div" className="history-more history-more-state" detail="Beginning of file history." />}
    {history.end === 'truncated' && <PanelState as="div" className="history-more history-more-state" detail="Showing the first 1,000 commits. Older history exists but is not available." />}
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
