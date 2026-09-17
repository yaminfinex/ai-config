import { createElement, useEffect, useRef, useState, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import type { ExtraProps } from 'react-markdown'
import { DiagramOverlay } from './DiagramOverlay.ts'
import { fenceInfo, mermaidLike, mermaidNotice, mermaidSourceLimit } from './mermaidLike.ts'
import { renderLeniently, renderMermaid } from './mermaidRender.ts'
import { useThemeType, type ThemeType } from './themeSignal.ts'

export type BlockMode = 'diagram' | 'source'

/**
 * A finished render, tied to the exact source and theme it was produced from: an SVG (with a
 * notice when it came from the lenient rewrite) or a notice alone for a failure.
 */
export type DiagramResult = { key: string, svg?: string, notice?: string }

export const diagramKey = (theme: ThemeType, source: string) => `${theme}\n${source}`

/**
 * What a diagram block shows (pure, tested): the SVG only when the last result matches the
 * current source and theme; otherwise the source. The notice is shown for a failure or an
 * oversized block (source shown) and for a lenient render (SVG shown); source mode has none.
 */
export function diagramView({ mode, key, result, tooLarge }: { mode: BlockMode, key: string, result: DiagramResult | undefined, tooLarge: boolean }) {
  const current = result?.key === key ? result : undefined
  const notice = mode !== 'diagram' ? undefined : tooLarge ? 'mermaid: block too large' : current?.notice
  const drawn = mode === 'diagram' && !tooLarge && current?.svg !== undefined
  return { drawn, notice, svg: drawn ? current?.svg : undefined }
}

export type FencedBlockProps = ComponentPropsWithoutRef<'pre'> & ExtraProps & { initialMode?: BlockMode, initialResult?: DiagramResult }

/**
 * Block chrome for every fenced code block (decision 4). Diagram-capable blocks get a
 * per-block mode switch and default to the drawn SVG; every other block is the plain
 * `<pre>` inside a wrapper that adds no height.
 */
export function FencedBlock({ node, children, initialMode, initialResult, ...props }: FencedBlockProps) {
  const { lang, source } = fenceInfo(node)
  const pre = createElement('pre', props, children)
  if (!mermaidLike(lang, source)) return createElement('div', { className: 'code-block' }, pre)
  return createElement(DiagramBlock, { source, initialMode: initialMode ?? 'diagram', initialResult, children: pre })
}

function DiagramBlock({ source, initialMode, initialResult, children }: { source: string, initialMode: BlockMode, initialResult?: DiagramResult, children: ReactNode }) {
  const [mode, setMode] = useState<BlockMode>(initialMode)
  const [open, setOpen] = useState(false)
  const maximise = useRef<HTMLButtonElement | null>(null)
  const theme = useThemeType()
  const tooLarge = source.length > mermaidSourceLimit
  const key = diagramKey(theme, source)
  const [result, setResult] = useState<DiagramResult | undefined>(initialResult)

  useEffect(() => {
    if (mode !== 'diagram' || tooLarge) return
    let cancelled = false
    renderLeniently(renderMermaid, source, theme)
      .then(({ svg, notice }) => { if (!cancelled) setResult({ key, svg, notice }) })
      .catch((error: unknown) => { if (!cancelled) setResult({ key, notice: mermaidNotice(error) }) })
    return () => { cancelled = true }
  }, [mode, source, theme, key, tooLarge])

  const view = diagramView({ mode, key, result, tooLarge })
  const modeButton = (value: BlockMode) => createElement('button', {
    type: 'button', className: mode === value ? 'active' : undefined, 'aria-pressed': mode === value, onClick: () => setMode(value),
  }, value)
  const close = () => { setOpen(false); maximise.current?.focus() }
  return createElement('div', { className: 'code-block code-block-diagram', 'data-mode': mode },
    createElement('div', { className: 'code-block-controls' },
      createElement('div', { className: 'detail-toggle code-block-mode', role: 'group', 'aria-label': 'Block rendering mode' }, modeButton('diagram'), modeButton('source')),
      view.drawn ? createElement('button', { ref: maximise, type: 'button', className: 'code-block-maximise', 'aria-label': 'Maximise diagram', onClick: () => setOpen(true) }, 'maximise') : null),
    view.notice ? createElement('div', { className: 'code-block-notice', role: 'status' }, view.notice) : null,
    // Mermaid's own sanitised output (securityLevel strict); no user HTML reaches this node (decision 6).
    view.drawn ? createElement('div', { className: 'mermaid-diagram', dangerouslySetInnerHTML: { __html: view.svg as string } }) : children,
    // The overlay shows the same SVG string; it closes with the drawing when the source or theme changes.
    open && view.drawn ? createPortal(createElement(DiagramOverlay, { svg: view.svg as string, onClose: close }), document.body) : null)
}
