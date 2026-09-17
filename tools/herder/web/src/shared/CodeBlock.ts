import { createElement, useEffect, useState, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import type { ExtraProps } from 'react-markdown'
import { fenceInfo, mermaidLike, mermaidNotice, mermaidSourceLimit } from './mermaidLike.ts'
import { renderMermaid } from './mermaidRender.ts'
import { useThemeType, type ThemeType } from './themeSignal.ts'

export type BlockMode = 'diagram' | 'source'

/** A finished render, tied to the exact source and theme it was produced from. */
export type DiagramResult = { key: string, svg?: string, notice?: string }

export const diagramKey = (theme: ThemeType, source: string) => `${theme}\n${source}`

/**
 * What a diagram block shows (pure, tested): the SVG only when the last result matches the
 * current source and theme; otherwise the source, with a notice when the result is an error
 * for this key or the block is too large.
 */
export function diagramView({ mode, key, result, tooLarge }: { mode: BlockMode, key: string, result: DiagramResult | undefined, tooLarge: boolean }) {
  const current = result?.key === key ? result : undefined
  const notice = mode !== 'diagram' ? undefined : tooLarge ? 'mermaid: block too large' : current?.notice
  const drawn = mode === 'diagram' && !notice && current?.svg !== undefined
  return { drawn, notice, svg: drawn ? current?.svg : undefined }
}

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
  const key = diagramKey(theme, source)
  const [result, setResult] = useState<DiagramResult>()

  useEffect(() => {
    if (mode !== 'diagram' || tooLarge) return
    let cancelled = false
    renderMermaid(source, theme)
      .then((svg) => { if (!cancelled) setResult({ key, svg }) })
      .catch((error: unknown) => { if (!cancelled) setResult({ key, notice: mermaidNotice(error) }) })
    return () => { cancelled = true }
  }, [mode, source, theme, key, tooLarge])

  const view = diagramView({ mode, key, result, tooLarge })
  const modeButton = (value: BlockMode) => createElement('button', {
    type: 'button', className: mode === value ? 'active' : undefined, 'aria-pressed': mode === value, onClick: () => setMode(value),
  }, value)
  return createElement('div', { className: 'code-block code-block-diagram', 'data-mode': mode },
    createElement('div', { className: 'detail-toggle code-block-mode', role: 'group', 'aria-label': 'Block rendering mode' }, modeButton('diagram'), modeButton('source')),
    view.notice ? createElement('div', { className: 'code-block-notice', role: 'status' }, view.notice) : null,
    // Mermaid's own sanitised output (securityLevel strict); no user HTML reaches this node (decision 6).
    view.drawn ? createElement('div', { className: 'mermaid-diagram', dangerouslySetInnerHTML: { __html: view.svg as string } }) : children)
}
