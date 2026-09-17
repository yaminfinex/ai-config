// Lazy mermaid loader: the library lands in its own chunk on the first diagram (decision 2).
// Every request is one serialized task ("initialize if the theme changed, then render") so no
// render ever sees another request's configuration, and every render call gets a fresh id.
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

const rootSvg = /^(\s*<svg\b[^>]*?)\swidth="100%"([^>]*?\bstyle="[^"]*?max-width:\s*([\d.]+)px[^"]*"[^>]*>)/u

/**
 * Mermaid emits `width="100%"` plus `style="max-width: <W>px"` on the root, which shrinks wide
 * diagrams to the box. Pinning the width to that value keeps the natural size so the box scrolls;
 * small diagrams are never upscaled because W is their own natural width.
 */
export function intrinsicWidth(svg: string): string {
  return svg.replace(rootSvg, (_match, head: string, tail: string, width: string) => `${head} width="${width}"${tail}`)
}

export const renderMermaid: MermaidRenderer = createMermaidRenderer(() => import('mermaid').then((mod) => mod.default as unknown as MermaidLike))
