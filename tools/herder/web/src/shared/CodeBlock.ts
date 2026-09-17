import { createElement, useEffect, useState, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import type { ExtraProps } from 'react-markdown'
import { fenceInfo, mermaidLike, mermaidNotice, mermaidSourceLimit } from './mermaidLike.ts'
import { renderMermaid } from './mermaidRender.ts'
import { useThemeType } from './themeSignal.ts'

export type BlockMode = 'diagram' | 'source'

type DiagramState = { svg?: string, notice?: string }

export type FencedBlockProps = ComponentPropsWithoutRef<'pre'> & ExtraProps & { initialMode?: BlockMode }

/**
 * Block chrome for every fenced code block (decision 4). Diagram-capable blocks get a
 * per-block mode switch and default to the drawn SVG; every other block is the plain
 * `<pre>` inside a wrapper that adds no height.
 */
export function FencedBlock({ node, children, initialMode, ...props }: FencedBlockProps) {
  const { lang, source } = fenceInfo(node)
  const pre = createElement('pre', props, children)
  if (!mermaidLike(lang, source)) return createElement('div', { className: 'code-block' }, pre)
  return createElement(DiagramBlock, { source, initialMode: initialMode ?? 'diagram', children: pre })
}

function DiagramBlock({ source, initialMode, children }: { source: string, initialMode: BlockMode, children: ReactNode }) {
  const [mode, setMode] = useState<BlockMode>(initialMode)
  const theme = useThemeType()
  const tooLarge = source.length > mermaidSourceLimit
  const [state, setState] = useState<DiagramState>({})

  useEffect(() => {
    if (mode !== 'diagram' || tooLarge) return
    let cancelled = false
    renderMermaid(source, theme)
      .then((svg) => { if (!cancelled) setState({ svg }) })
      .catch((error: unknown) => { if (!cancelled) setState({ notice: mermaidNotice(error) }) })
    return () => { cancelled = true }
  }, [mode, source, theme, tooLarge])

  const notice = tooLarge ? 'mermaid: block too large' : state.notice
  const drawn = mode === 'diagram' && !notice && state.svg !== undefined
  const modeButton = (value: BlockMode) => createElement('button', {
    type: 'button', className: mode === value ? 'active' : undefined, 'aria-pressed': mode === value, onClick: () => setMode(value),
  }, value)
  return createElement('div', { className: 'code-block code-block-diagram', 'data-mode': mode },
    createElement('div', { className: 'detail-toggle code-block-mode', role: 'group', 'aria-label': 'Block rendering mode' }, modeButton('diagram'), modeButton('source')),
    mode === 'diagram' && notice ? createElement('div', { className: 'code-block-notice', role: 'status' }, notice) : null,
    // Mermaid's own sanitised output (securityLevel strict); no user HTML reaches this node (decision 6).
    drawn ? createElement('div', { className: 'mermaid-diagram', dangerouslySetInnerHTML: { __html: state.svg as string } }) : children)
}
