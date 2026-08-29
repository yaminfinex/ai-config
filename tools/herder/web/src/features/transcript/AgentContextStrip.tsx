import { useEffect, useRef, useState } from 'react'
import { AgentStatusDot } from '../../shared/presentation'
import { agentContextPresentation, hasRightOverflow } from '../../shared/agentContext'
import type { AgentDetail } from '../../types'
import type { FolderTarget } from '../../types'
import { useQuery } from '@tanstack/react-query'
import { queryKeys, resolveFiles } from '../../api/client'
import { cwdFolderTarget, exactRootChangesTarget } from '../folders/folderModel'
import { openInSideLabel, placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { useDOMEvent, useSizeObserver } from '../../shared/lifecycle'

export function AgentContextStrip({ agent, liveStatus, onOpenFolder, onOpenChanges }: { agent?: AgentDetail, liveStatus: string, onOpenFolder: (target: FolderTarget, placement?: OpenPlacement) => void, onOpenChanges: (root: string, placement?: OpenPlacement) => void }) {
  const context = agent ? agentContextPresentation(agent, liveStatus) : undefined
  const cwd = agent?.cwd
  const cwdResolution = useQuery({
    queryKey: queryKeys.resolve(cwd ?? ''),
    queryFn: ({ signal }) => resolveFiles(cwd ?? '', undefined, fetch, signal),
    enabled: Boolean(cwd),
    retry: false,
  })
  const cwdTarget = cwd && cwdResolution.data ? cwdFolderTarget(cwd, cwdResolution.data.roots) : null
  const changesRoot = exactRootChangesTarget(cwdTarget)
  const sideHint = openInSideLabel(navigator.userAgent)
  const cwdReason = cwdResolution.isPending ? 'Verifying this working directory against served roots…'
    : cwdResolution.error ? `Working folder unavailable: ${cwdResolution.error.message}`
      : !cwdTarget ? 'This working directory is not under any served root.' : `Open ${cwd}`
  const changesReason = changesRoot ? `Open changes for ${changesRoot}` : cwdTarget ? 'Changes requires the agent working directory to be an exact served root.' : cwdReason
  const innerRef = useRef<HTMLDivElement>(null)
  const [overflowing, setOverflowing] = useState(false)
  const updateOverflow = (inner: HTMLDivElement) => setOverflowing(hasRightOverflow(inner.scrollWidth, inner.clientWidth, inner.scrollLeft))
  useSizeObserver(innerRef, updateOverflow, Boolean(agent), agent, (inner) => inner.parentElement ? [inner.parentElement] : [])
  useDOMEvent(innerRef, 'scroll', () => { if (innerRef.current) updateOverflow(innerRef.current) }, { passive: true }, Boolean(agent))
  useEffect(() => { if (!agent) setOverflowing(false) }, [agent])
  return <section className="agent-context-strip" data-overflow-right={overflowing || undefined} aria-label="Agent context" aria-busy={!agent}>
    {context && <div className="agent-context-strip-inner" ref={innerRef}>
      {context.cwd && <span className="context-cwd-wrap" title={`${cwdReason} · ${sideHint}`}><button type="button" className="context-fact context-cwd" disabled={!cwdTarget} onClick={(event) => { if (cwdTarget) onOpenFolder(cwdTarget, placementFromModifiers(event)) }}><span>cwd</span>{context.cwd.display}<span aria-hidden="true">↗</span></button></span>}
      {context.repository && <span className="context-fact context-repository" title={context.repository.remote}><span>repo</span>{context.repository.display}</span>}
      {context.repository && <span title={`${changesReason} · ${sideHint}`}><button type="button" className="context-fact context-changes" disabled={!changesRoot} onClick={(event) => { if (changesRoot) onOpenChanges(changesRoot, placementFromModifiers(event)) }}><span aria-hidden="true">±</span>changes</button></span>}
      <span className="context-status" title={`Bus status: ${context.status !== '-' ? context.status : 'unknown'}`}><AgentStatusDot status={context.status} /></span>
      {context.details.map((detail, index) => <span className="context-detail" key={`${index}:${detail}`}>{detail}</span>)}
      {context.vitals.map((vital, index) => <span className="context-vital" key={`${index}:${vital}`}>{vital}</span>)}
    </div>}
  </section>
}
