import { AgentStatusDot } from '../../shared/presentation'
import { agentContextPresentation } from '../../shared/agentContext'
import type { AgentDetail } from '../../types'

export function AgentContextStrip({ agent, liveStatus }: { agent?: AgentDetail, liveStatus: string }) {
  const context = agent ? agentContextPresentation(agent, liveStatus) : undefined
  return <section className="agent-context-strip" aria-label="Agent context" aria-busy={!agent}>
    {context && <div className="agent-context-strip-inner">
      {context.cwd && <span className="context-fact context-cwd" title={context.cwd.full}><span>cwd</span>{context.cwd.display}</span>}
      {context.repository && <span className="context-fact context-repository" title={context.repository.remote}><span>repo</span>{context.repository.display}</span>}
      <span className="context-status"><AgentStatusDot status={context.status} />{context.status !== '-' ? context.status : 'unknown'}</span>
      {context.details.map((detail, index) => <span className="context-detail" key={`${index}:${detail}`}>{detail}</span>)}
      {context.vitals.map((vital, index) => <span className="context-vital" key={`${index}:${vital}`}>{vital}</span>)}
    </div>}
  </section>
}
