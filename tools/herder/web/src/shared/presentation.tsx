import { useEffect, useReducer } from 'react'
import { agentStatusPresentation } from './agentStatus.ts'
import { bannerSemantics, type BannerTone } from './bannerPresentation.ts'
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

export function Banner({ source, detail, tone = 'error' }: { source: string, detail: string, tone?: BannerTone }) {
  const key = `${tone}\u0000${source}\u0000${detail}`
  const [state, dispatch] = useReducer(bannerState, { key, dismissed: false })
  useEffect(() => dispatch({ type: 'sync', key }), [key])
  if (state.key === key && state.dismissed) return null
  const presentation = bannerSemantics(tone)
  return <div className={presentation.className} role={presentation.role}><strong>{source}</strong><span>{detail}</span>{presentation.dismissible && <button type="button" className="dismiss-banner" aria-label={`Dismiss ${source} error`} onClick={() => dispatch({ type: 'dismiss' })}>×</button>}</div>
}
