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

/**
 * A sequenceDiagram line whose text after the first `:` mermaid still tokenises: a message
 * (any `-`/`--` arrow before the colon) or a Note. Group 1 is the structural head, group 2 the text.
 */
const sequenceTextLine = /^(\s*(?:[Nn]ote\s+(?:over|left of|right of)\b[^:]*|[^:]*?-{1,2}(?:>>|>|x|\))[^:]*):)(.*)$/u

export type LenientMermaid = { source: string, changes: number }

/**
 * The lenient rewrite, applied only after mermaid rejected the literal source (decision 5).
 * In a sequenceDiagram, `;` inside message or Note text becomes `#59;`: mermaid's sequence
 * lexer ends the statement at a `;` even after the colon ("Expecting ... got ','"), and the
 * entity draws the character. A final `;` on the line is the legal terminator and stays, as
 * does the `;` closing an entity such as `#59;`. Every other diagram type accepts `;` in
 * labels (verified against mermaid 11.17.2), so nothing else is touched.
 */
export function lenientMermaid(source: string): LenientMermaid {
  const lines = source.split('\n')
  const first = lines.find((line) => line.trim() !== '')?.trim() ?? ''
  if (!first.startsWith('sequenceDiagram')) return { source, changes: 0 }
  let changes = 0
  const rewritten = lines.map((line) => line.replace(sequenceTextLine, (_line, head: string, text: string) =>
    head + text.replace(/(#\w+)?;(?!\s*$)/gu, (match, entity: string | undefined) => {
      if (entity) return match
      changes += 1
      return '#59;'
    })))
  return changes === 0 ? { source, changes: 0 } : { source: rewritten.join('\n'), changes }
}

/** The notice above a diagram drawn from the lenient rewrite, so the drawing is known not to be the literal source. */
export function lenientNotice(changes: number): string {
  return `mermaid: drawn after escaping ${changes} character${changes === 1 ? '' : 's'} that mermaid rejects`
}
