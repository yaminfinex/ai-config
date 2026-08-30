export type AssistantFenceSegment =
  | { kind: 'text', content: string }
  | { kind: 'internal', content: string, wordCount: number }
  | { kind: 'status', content: string }

export type AssistantFencing = {
  fenced: boolean
  hasVisibleText: boolean
  segments: AssistantFenceSegment[]
}

const fenceTag = /<\/?(?:internal|status)>/g

function literal(content: string): AssistantFencing {
  return { fenced: false, hasVisibleText: Boolean(content.trim()), segments: [{ kind: 'text', content }] }
}

function internalWordCount(content: string) {
  return content.match(/\S+/g)?.length ?? 0
}

export function parseAssistantFencing(content: string): AssistantFencing {
  const segments: AssistantFenceSegment[] = []
  let cursor = 0
  let open: { kind: 'internal' | 'status', bodyStart: number } | undefined
  let fenced = false

  for (const match of content.matchAll(fenceTag)) {
    const tag = match[0]
    const index = match.index
    const closing = tag.startsWith('</')
    const kind = tag.includes('internal') ? 'internal' : 'status'

    if (!open) {
      if (closing) return literal(content)
      if (index > cursor) segments.push({ kind: 'text', content: content.slice(cursor, index) })
      open = { kind, bodyStart: index + tag.length }
      fenced = true
      continue
    }

    if (!closing || kind !== open.kind) return literal(content)
    const body = content.slice(open.bodyStart, index)
    if (kind === 'status' && /[\r\n]/.test(body)) return literal(content)
    segments.push(kind === 'internal'
      ? { kind, content: body, wordCount: internalWordCount(body) }
      : { kind, content: body })
    cursor = index + tag.length
    open = undefined
  }

  if (open) return literal(content)
  if (!fenced) return literal(content)
  if (cursor < content.length) segments.push({ kind: 'text', content: content.slice(cursor) })
  return { fenced: true, hasVisibleText: segments.some((segment) => segment.kind === 'text' && Boolean(segment.content.trim())), segments }
}
