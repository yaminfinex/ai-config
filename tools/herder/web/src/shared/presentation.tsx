import { useEffect, useReducer, type ReactElement } from 'react'
import { agentStatusPresentation, agentToolBadge } from './agentStatus.ts'
import { bannerSemantics, type BannerTone } from './bannerPresentation.ts'
import { bannerState } from './bannerState.ts'
export { workspaceName } from './workspaceName.ts'

export function AgentStatusDot({ status }: { status: string }) {
  const presentation = agentStatusPresentation(status)
  const description = `${presentation.label}: ${presentation.meaning}`
  return <span className={`status-dot ${presentation.className}`} title={description} aria-label={description} />
}

// Brand-style glyphs stay abstract on purpose: an eight-ray spark for
// claude and a hexagon for codex, not trademarked artwork.
const toolGlyphs: Record<string, ReactElement> = {
  claude: <svg viewBox="0 0 16 16" width="10" height="10" aria-hidden="true">
    <g stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" fill="none">
      <path d="M8 1.5v13M1.5 8h13M3.4 3.4l9.2 9.2M12.6 3.4l-9.2 9.2" />
    </g>
  </svg>,
  codex: <svg viewBox="0 0 16 16" width="10" height="10" aria-hidden="true">
    <polygon points="8,1.8 13.4,4.9 13.4,11.1 8,14.2 2.6,11.1 2.6,4.9" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
  </svg>,
}

export function ToolBadge({ tool }: { tool: string | undefined }) {
  const fallback = agentToolBadge(tool)
  if (!fallback) return null
  const glyph = tool ? toolGlyphs[tool] : undefined
  if (glyph) return <span className="tool-badge glyph" title={tool} aria-label={tool}>{glyph}</span>
  return <span className="tool-badge" title={tool} aria-label={tool}>{fallback}</span>
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
