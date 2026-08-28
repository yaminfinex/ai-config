import { useEffect, useRef, useState } from 'react'
import { AgentStatusDot } from '../../shared/presentation'
import { agentContextPresentation, hasRightOverflow } from '../../shared/agentContext'
import type { AgentDetail } from '../../types'

export function AgentContextStrip({ agent, liveStatus }: { agent?: AgentDetail, liveStatus: string }) {
  const context = agent ? agentContextPresentation(agent, liveStatus) : undefined
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
      {context.cwd && <span className="context-fact context-cwd" title={context.cwd.full}><span>cwd</span>{context.cwd.display}</span>}
      {context.repository && <span className="context-fact context-repository" title={context.repository.remote}><span>repo</span>{context.repository.display}</span>}
      <span className="context-status"><AgentStatusDot status={context.status} />{context.status !== '-' ? context.status : 'unknown'}</span>
      {context.details.map((detail, index) => <span className="context-detail" key={`${index}:${detail}`}>{detail}</span>)}
      {context.vitals.map((vital, index) => <span className="context-vital" key={`${index}:${vital}`}>{vital}</span>)}
    </div>}
  </section>
}
