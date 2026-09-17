// Pure detection of fenced blocks that mermaid should draw (brief decision 3).
// A block is a diagram when its info tag is `mermaid`, or when it is untagged
// and its first non-blank line starts with a known mermaid diagram keyword.

export const mermaidKeywords = [
  'graph', 'flowchart', 'sequenceDiagram', 'classDiagram', 'stateDiagram', 'stateDiagram-v2', 'erDiagram',
  'journey', 'gantt', 'pie', 'mindmap', 'timeline', 'gitGraph', 'quadrantChart', 'xychart-beta', 'block-beta',
  'sankey-beta', 'requirementDiagram', 'C4Context',
] as const

/** Blocks longer than this render as source with a notice instead of being handed to mermaid (decision 6). */
export const mermaidSourceLimit = 20_000

export function mermaidLike(lang: string | undefined, source: string): boolean {
  const tag = (lang ?? '').trim().toLowerCase()
  if (tag === 'mermaid') return true
  if (tag !== '') return false
  const first = source.split('\n').find((line) => line.trim() !== '')?.trim()
  if (!first) return false
  const head = first.split(/[\s;]/u, 1)[0]
  return mermaidKeywords.some((keyword) => head === keyword)
}

export type FenceInfo = { lang: string | undefined, source: string }

type HastText = { type?: string, value?: string }
type HastElement = { type?: string, tagName?: string, properties?: { className?: unknown }, children?: HastText[] }

/** Reads the language tag and text out of react-markdown's hast `pre` node without touching React children. */
export function fenceInfo(node: unknown): FenceInfo {
  const pre = node as { children?: HastElement[] } | undefined
  const code = pre?.children?.find((child) => child.type === 'element' && child.tagName === 'code')
  const classes = code?.properties?.className
  const classList = Array.isArray(classes) ? classes.map(String) : typeof classes === 'string' ? classes.split(/\s+/u) : []
  const lang = classList.find((name) => name.startsWith('language-'))?.slice('language-'.length)
  const source = (code?.children ?? []).filter((child) => child.type === 'text').map((child) => child.value ?? '').join('')
  return { lang, source }
}

/** First line of a render failure, for the one-line notice above the source (decision 5). */
export function mermaidNotice(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error)
  const first = message.split('\n').find((line) => line.trim() !== '')?.trim() ?? 'render failed'
  return `mermaid: ${first}`
}
