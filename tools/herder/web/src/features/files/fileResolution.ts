import type { FileCandidate, ResolveResponse } from '../../types'

// Implementation-tunable: fzf contributes 16 points per rune before bonuses.
export const FUZZY_POPOVER_SCORE_PER_RUNE = 20

export const structuralDelimiter = /[\s()[\]{}<>]/u
const enclosingDelimiters = ['`', '"', "'"] as const
const immediatePathSignal = /^(?:\/|~\/|\.\.?\/|[^\s()[\]{}<>`"':/\\]+\/)/u

export function isRenderedInlineCode(target: Pick<Element, 'closest'>) {
  return Boolean(target.closest('code')) && !target.closest('pre')
}

export function pathTokenSpanAt(text: string, offset: number, renderedCode = false) {
  const point = Math.max(0, Math.min(text.length, offset))
  if (renderedCode) return { start: 0, end: text.length, text }
  for (const delimiter of enclosingDelimiters) {
    const left = text.lastIndexOf(delimiter, point)
    const right = text.indexOf(delimiter, point)
    if (left >= 0 && right > left && immediatePathSignal.test(text.slice(left + 1, right))) return { start: left, end: right + 1, text: text.slice(left, right + 1) }
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
  const line = Number(match?.[1])
  return Number.isInteger(line) && line >= 1 ? { line } : {}
}

export function hasPathSignal(mention: string, codeOrQuoted: boolean) {
  const value = unwrappedMention(mention).replace(/[),;!?]+$/u, '')
  return codeOrQuoted || /[/\\]/u.test(value) || /:\d+$/u.test(value) || /(?:^|[^.])\.[\p{L}\p{N}][\p{L}\p{N}._-]*$/u.test(value)
}

// The quiet line under a capped result list; null when nothing was cut.
export function resultsLimitLine(shown: number, total: number) {
  return total > shown ? `${shown} of ${total} — type more to narrow` : null
}

// The server caps candidates after ranking. Suffix shares a band with prefix,
// so a cut list proves "exactly one certain hit" only when the cut fell among
// fuzzy hits, which rank after every other tier and anchor band.
export function autoOpenCandidate(resolution: ResolveResponse): FileCandidate | null {
  if (resolution.roots.some((root) => root.status !== 'complete')) return null
  const cut = (resolution.total ?? 0) > resolution.candidates.length
  if (cut && resolution.candidates.at(-1)?.tier !== 'fuzzy') return null
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

// The quiet "+N worktrees" after a root label: the server showed this file
// once for its repository and folded N other checkouts' copies into it.
export function alsoLabel(candidate: Pick<FileCandidate, 'also'>) {
  const also = candidate.also ?? 0
  return also > 0 ? `+${also} ${also === 1 ? 'worktree' : 'worktrees'}` : null
}

export function rootTitle(candidate: Pick<FileCandidate, 'root' | 'also' | 'also_roots'>) {
  const others = candidate.also_roots ?? []
  if (others.length === 0) return candidate.root
  const unlisted = (candidate.also ?? 0) - others.length
  return [candidate.root, 'also in:', ...others, ...(unlisted > 0 ? [`and ${unlisted} more`] : [])].join('\n')
}

export function rootLabel(root: string) {
  const parts = root.split('/').filter(Boolean)
  return parts.at(-1) || root
}
