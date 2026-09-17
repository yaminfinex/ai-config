// Lazy mermaid loader: the library lands in its own chunk on the first diagram (decision 2).
// Every request is one serialized task ("initialize if the theme changed, then render") so no
// render ever sees another request's configuration, and every render call gets a fresh id.
import { lenientMermaid, lenientNotice } from './mermaidLike.ts'
import type { ThemeType } from './themeSignal.ts'

export type MermaidLike = {
  initialize: (config: { startOnLoad: boolean, securityLevel: 'strict', suppressErrorRendering: boolean, theme: 'default' | 'dark' }) => void
  render: (id: string, source: string) => Promise<{ svg: string }>
}

export type MermaidRenderer = (source: string, theme: ThemeType) => Promise<string>

/** Builds a renderer over a loader; the loader's promise is shared and independent of theme. */
export function createMermaidRenderer(load: () => Promise<MermaidLike>): MermaidRenderer {
  let modulePromise: Promise<MermaidLike> | undefined
  let initializedTheme: ThemeType | undefined
  let renderSerial = 0
  let queue: Promise<unknown> = Promise.resolve()
  return (source, theme) => {
    const task = queue.then(async () => {
      modulePromise ??= load()
      const mermaid = await modulePromise
      if (initializedTheme !== theme) {
        mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true, theme: theme === 'light' ? 'default' : 'dark' })
        initializedTheme = theme
      }
      renderSerial += 1
      const { svg } = await mermaid.render(`herder-mermaid-${renderSerial}`, source)
      return intrinsicWidth(svg)
    })
    queue = task.catch(() => undefined)
    return task
  }
}

const rootSvg = /^(\s*<svg\b[^>]*?)\swidth="100%"([^>]*?)\sstyle="([^"]*?)max-width:\s*([\d.]+)px;?([^"]*)"([^>]*>)/u

/**
 * Mermaid emits `width="100%"` plus an inline `style="max-width: <W>px"` on the root. Pinning
 * the width attribute to W and dropping the inline max-width leaves the natural size on the
 * element (the overlay measures it) and hands the fit to the stylesheet: `.mermaid-diagram svg`
 * carries `max-width: 100%; height: auto`, so a wide diagram scales down to the box and a
 * narrow one keeps its own width (decision 1). An inline max-width would beat that rule.
 */
export function intrinsicWidth(svg: string): string {
  return svg.replace(rootSvg, (_match, head: string, mid: string, before: string, width: string, after: string, tail: string) => {
    const style = `${before}${after}`.replace(/\s+/gu, ' ').trim()
    return `${head} width="${width}"${mid}${style ? ` style="${style}"` : ''}${tail}`
  })
}

export const renderMermaid: MermaidRenderer = createMermaidRenderer(() => import('mermaid').then((mod) => mod.default as unknown as MermaidLike))

export type DiagramRender = { svg: string, notice?: string }

/**
 * Renders the literal source; when mermaid rejects it, retries once with the lenient rewrite
 * and says what was escaped (decision 5). The original error surfaces when there is nothing
 * to rewrite or when the retry fails too.
 */
export async function renderLeniently(render: MermaidRenderer, source: string, theme: ThemeType): Promise<DiagramRender> {
  try {
    return { svg: await render(source, theme) }
  } catch (error) {
    const lenient = lenientMermaid(source)
    if (lenient.changes === 0) throw error
    try {
      return { svg: await render(lenient.source, theme), notice: lenientNotice(lenient.changes) }
    } catch {
      throw error
    }
  }
}
