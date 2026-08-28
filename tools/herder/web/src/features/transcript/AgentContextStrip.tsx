import { useEffect, useRef, useState } from 'react'
import { AgentStatusDot } from '../../shared/presentation'
import { agentContextPresentation, hasRightOverflow } from '../../shared/agentContext'
import type { AgentDetail } from '../../types'
import type { FolderTarget } from '../../types'
import { useQuery } from '@tanstack/react-query'
import { queryKeys, resolveFiles } from '../../api/client'
import { cwdFolderTarget } from '../folders/folderModel'

export function AgentContextStrip({ agent, liveStatus, onOpenFolder }: { agent?: AgentDetail, liveStatus: string, onOpenFolder: (target: FolderTarget) => void }) {
  const context = agent ? agentContextPresentation(agent, liveStatus) : undefined
  const cwd = agent?.cwd
  const cwdResolution = useQuery({
    queryKey: queryKeys.resolve(cwd ?? ''),
    queryFn: ({ signal }) => resolveFiles(cwd ?? '', undefined, fetch, signal),
    enabled: Boolean(cwd),
    retry: false,
  })
  const cwdTarget = cwd && cwdResolution.data ? cwdFolderTarget(cwd, cwdResolution.data.roots) : null
  const cwdReason = cwdResolution.isPending ? 'Verifying this working directory against served roots…'
    : cwdResolution.error ? `Working folder unavailable: ${cwdResolution.error.message}`
      : !cwdTarget ? 'This working directory is not under any served root.' : `Open ${cwd}`
  const innerRef = useRef<HTMLDivElement>(null)
  const [overflowing, setOverflowing] = useState(false)
  useEffect(() => {
    const inner = innerRef.current
    if (!inner) { setOverflowing(false); return }
    const update = () => setOverflowing(hasRightOverflow(inner.scrollWidth, inner.clientWidth, inner.scrollLeft))
    update()
    const resize = new ResizeObserver(update)
    resize.observe(inner)
    if (inner.parentElement) resize.observe(inner.parentElement)
    inner.addEventListener('scroll', update, { passive: true })
    return () => { resize.disconnect(); inner.removeEventListener('scroll', update) }
  }, [agent, liveStatus])
  return <section className="agent-context-strip" data-overflow-right={overflowing || undefined} aria-label="Agent context" aria-busy={!agent}>
    {context && <div className="agent-context-strip-inner" ref={innerRef}>
      {context.cwd && <span className="context-cwd-wrap" title={cwdReason}><button type="button" className="context-fact context-cwd" disabled={!cwdTarget} onClick={() => { if (cwdTarget) onOpenFolder(cwdTarget) }}><span>cwd</span>{context.cwd.display}<span aria-hidden="true">↗</span></button></span>}
      {context.repository && <span className="context-fact context-repository" title={context.repository.remote}><span>repo</span>{context.repository.display}</span>}
      <span className="context-status"><AgentStatusDot status={context.status} />{context.status !== '-' ? context.status : 'unknown'}</span>
      {context.details.map((detail, index) => <span className="context-detail" key={`${index}:${detail}`}>{detail}</span>)}
      {context.vitals.map((vital, index) => <span className="context-vital" key={`${index}:${vital}`}>{vital}</span>)}
    </div>}
  </section>
}
