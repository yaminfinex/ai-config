import type { FileCandidate, ResolveResponse } from '../../types'

// Implementation-tunable: fzf contributes 16 points per rune before bonuses.
export const FUZZY_POPOVER_SCORE_PER_RUNE = 20

const structuralDelimiter = /[\s()[\]{}<>]/u
const enclosingDelimiters = ['`', '"', "'"] as const

export function isRenderedInlineCode(target: Pick<Element, 'closest'>) {
  return Boolean(target.closest('code')) && !target.closest('pre')
}

export function pathTokenSpanAt(text: string, offset: number, renderedCode = false) {
  const point = Math.max(0, Math.min(text.length, offset))
  if (renderedCode) return { start: 0, end: text.length, text }
  for (const delimiter of enclosingDelimiters) {
    const left = text.lastIndexOf(delimiter, point)
    const right = text.indexOf(delimiter, point)
    const delimitersBefore = left < 0 ? 0 : text.slice(0, left).split(delimiter).length - 1
    if (left >= 0 && delimitersBefore % 2 === 0 && right > left && !text.slice(left + 1, right).includes(delimiter)) return { start: left, end: right + 1, text: text.slice(left, right + 1) }
  }
  let start = point
  let end = point
  while (start > 0 && !structuralDelimiter.test(text[start - 1])) start--
  while (end < text.length && !structuralDelimiter.test(text[end])) end++
  return { start, end, text: text.slice(start, end) }
}

function unwrappedMention(mention: string) {
  const trimmed = mention.trim()
  const first = trimmed[0]
  return first && enclosingDelimiters.includes(first as typeof enclosingDelimiters[number]) && trimmed.endsWith(first)
    ? trimmed.slice(1, -1)
    : trimmed
}

export function mentionLine(mention: string): { line?: number } {
  const value = unwrappedMention(mention)
  const withoutTrailing = value.replace(/[),.;!?]+$/u, '')
  const match = withoutTrailing.match(/:(\d+)$/u)
  return match ? { line: Number(match[1]) } : {}
}

export function hasPathSignal(mention: string, codeOrQuoted: boolean) {
  const value = unwrappedMention(mention).replace(/[),;!?]+$/u, '')
  return codeOrQuoted || /[/\\]/u.test(value) || /:\d+$/u.test(value) || /(?:^|[^.])\.[\p{L}\p{N}][\p{L}\p{N}._-]*$/u.test(value)
}

export function autoOpenCandidate(resolution: ResolveResponse): FileCandidate | null {
  if (resolution.roots.some((root) => root.status !== 'complete')) return null
  const certain = resolution.candidates.filter((candidate) => candidate.tier === 'exact' || candidate.tier === 'suffix')
  return certain.length === 1 ? certain[0] : null
}

export function keyboardCandidate(resolution: ResolveResponse, visible: FileCandidate[], activeIndex: number) {
  return activeIndex >= 0 && visible[activeIndex] ? visible[activeIndex] : autoOpenCandidate(resolution)
}

export function fileFailureKind(status?: number, error?: string): 'vanished' | 'unknown-root' | 'other' {
  if (status === 404 && error === 'not found') return 'vanished'
  if (status === 404 && error === 'unknown root') return 'unknown-root'
  return 'other'
}

export function quickOpenAgentPreference(name: string, status: string) {
  return ['active', 'listening', 'blocked'].includes(status) ? name : undefined
}

export function isConfidentResolution(resolution: ResolveResponse, query: string) {
  const top = resolution.candidates[0]
  if (!top) return false
  if (top.tier !== 'fuzzy') return true
  const scoredQuery = unwrappedMention(query).replace(/[),.;!?]+$/u, '').replace(/:\d+$/u, '')
  return top.score >= FUZZY_POPOVER_SCORE_PER_RUNE * [...scoredQuery].length
}

export function rootLabel(root: string) {
  const parts = root.split('/').filter(Boolean)
  return parts.at(-1) || root
}
