import { agentStatusPresentation } from './agentStatus.ts'
export { workspaceName } from './workspaceName.ts'

export function AgentStatusDot({ status }: { status: string }) {
  const presentation = agentStatusPresentation(status)
  const description = `${presentation.label}: ${presentation.meaning}`
  return <span className={`status-dot ${presentation.className}`} title={description} aria-label={description} />
}

export function gapLabel(gap: string) {
  if (gap === '-') return ''
  return gap.toLowerCase().includes('pane') ? 'no pane' : 'gap'
}

export function Banner({ source, detail }: { source: string, detail: string }) {
  return <div className="banner" role="alert"><strong>{source}</strong><span>{detail}</span></div>
}
