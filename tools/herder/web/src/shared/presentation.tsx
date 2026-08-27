import { useEffect, useReducer } from 'react'
import { agentStatusPresentation } from './agentStatus.ts'
import { bannerState } from './bannerState.ts'
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
  const key = `${source}\u0000${detail}`
  const [state, dispatch] = useReducer(bannerState, { key, dismissed: false })
  useEffect(() => dispatch({ type: 'sync', key }), [key])
  if (state.key === key && state.dismissed) return null
  return <div className="banner" role="alert"><strong>{source}</strong><span>{detail}</span><button type="button" className="dismiss-banner" aria-label={`Dismiss ${source} error`} onClick={() => dispatch({ type: 'dismiss' })}>×</button></div>
}
