// Lazy mermaid loader: the library lands in its own chunk on the first diagram (decision 2).
import type { ThemeType } from './themeSignal.ts'

type MermaidModule = typeof import('mermaid')['default']

let modulePromise: Promise<MermaidModule> | undefined
let initializedTheme: ThemeType | undefined
let renderSerial = 0

async function loadMermaid(theme: ThemeType): Promise<MermaidModule> {
  modulePromise ??= import('mermaid').then((mod) => mod.default)
  const mermaid = await modulePromise
  if (initializedTheme !== theme) {
    mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true, theme: theme === 'light' ? 'default' : 'dark' })
    initializedTheme = theme
  }
  return mermaid
}

const cacheLimit = 64
const svgCache = new Map<string, string>()

/** Renders one diagram to an SVG string; ids are unique per call so concurrent blocks never collide. */
export async function renderMermaid(source: string, theme: ThemeType): Promise<string> {
  const key = `${theme}\n${source}`
  const cached = svgCache.get(key)
  if (cached !== undefined) return cached
  const mermaid = await loadMermaid(theme)
  renderSerial += 1
  const { svg } = await mermaid.render(`herder-mermaid-${renderSerial}`, source)
  if (svgCache.size >= cacheLimit) svgCache.delete(svgCache.keys().next().value as string)
  svgCache.set(key, svg)
  return svg
}
