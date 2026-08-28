import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiProblem, getFile, queryKeys, resolveFiles } from '../../api/client'
import type { FileTarget, FolderTarget } from '../../types'
import { Banner } from '../../shared/presentation'
import { fileMarkdownComponents, Markdown } from '../../shared/Markdown'
import { FileResults } from './FileResults'
import { fileFailureKind, rootLabel } from './fileResolution'
import { isMarkdownPath, type FileViewMode } from './fileTabs'
import { candidateDestination, missionFacts, missionMarkdownBody } from '../folders/folderModel'

function formattedBytes(size: number) {
  return `${size.toLocaleString()} bytes`
}

export function FilePanel({ target, viewMode, onViewMode, onOpenFile, onOpenFolder }: {
  target: FileTarget
  viewMode: FileViewMode
  onViewMode: (mode: FileViewMode) => void
  onOpenFile: (target: FileTarget) => void
  onOpenFolder: (target: FolderTarget) => void
}) {
  const targetLineRef = useRef<HTMLElement | null>(null)
  const fileQuery = useQuery({
    queryKey: queryKeys.file(target.root, target.path),
    queryFn: ({ signal }) => getFile(target.root, target.path, fetch, signal),
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

  useEffect(() => {
    if (viewMode !== 'source' || !target.line || !fileQuery.data || fileQuery.data.binary) return
    requestAnimationFrame(() => targetLineRef.current?.scrollIntoView({ block: 'center' }))
  }, [fileQuery.data, target.line, viewMode])

  const data = fileQuery.data
  const markdown = isMarkdownPath(target.path)
  const missionMarkdown = Boolean(data && !data.binary && /(?:^|\/)mission\.md$/iu.test(target.path))
  const facts = data && !data.binary && missionMarkdown ? missionFacts(data.content) : null
  const hasFacts = facts && Object.keys(facts).length > 0
  return <main className="file-panel">
    <header className="file-header">
      <div className="file-title"><strong>{rootLabel(target.path)}</strong><span>{target.path}</span><span className="root-path" title={target.root}>{target.root}</span></div>
      {markdown && <div className="detail-toggle file-view-toggle" aria-label="Markdown view">
        <button type="button" className={viewMode === 'rendered' ? 'active' : ''} aria-pressed={viewMode === 'rendered'} onClick={() => onViewMode('rendered')}>Rendered</button>
        <button type="button" className={viewMode === 'source' ? 'active' : ''} aria-pressed={viewMode === 'source'} onClick={() => onViewMode('source')}>Source</button>
      </div>}
      <button type="button" onClick={() => fileQuery.refetch()} disabled={fileQuery.isFetching}>{fileQuery.isFetching ? 'Refreshing…' : 'Refresh'}</button>
    </header>
    {fileQuery.isPending && <div className="file-state" role="status">Reading current file…</div>}
    {failure && !needsAlternatives && <Banner source="file" detail={`${failure.problem.error}: ${failure.problem.detail}`} />}
    {needsAlternatives && <section className="file-state vanished" role="status"><strong>{vanished ? 'File vanished' : 'Root no longer served'}</strong><p>{vanished ? 'This path no longer exists in its root.' : 'This file root is no longer in the live readable universe.'} Closest current matches:</p>
      {alternatives.isPending && <p>Resolving current files…</p>}
      {alternatives.error && <Banner source="resolve" detail={alternatives.error.message} />}
      {!alternatives.isFetching && !alternatives.error && <FileResults resolution={alternatives.data} limit={8} onSelect={(candidate) => {
        if (candidateDestination(candidate) === 'folder') onOpenFolder({ root: candidate.root, path: candidate.path })
        else onOpenFile({ root: candidate.root, path: candidate.path, line: target.line })
      }} />}
    </section>}
    {data && !failure && <>
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
          {markdown && viewMode === 'rendered' ? <div className="markdown file-markdown"><Markdown components={fileMarkdownComponents}>{missionMarkdown ? missionMarkdownBody(data.content) : data.content}</Markdown></div> : <pre className="file-source">{data.content.split('\n').map((line, index) => {
            const number = index + 1
            return <span className={`file-line${number === target.line ? ' target-line' : ''}`} ref={number === target.line ? targetLineRef : undefined} key={number}>
              <span className="line-number" aria-hidden="true">{number}</span><span className="line-text">{line || ' '}</span>{'\n'}
            </span>
          })}</pre>
          }
        </div>}
    </>}
  </main>
}
