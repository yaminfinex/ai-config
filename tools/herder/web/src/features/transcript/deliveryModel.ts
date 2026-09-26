import { isWebOperatorMessage, polishHcomDeliveryText } from '../../messagePolish.ts'

type DeliveryValue = Record<string, unknown>

const text = (value: unknown) => typeof value === 'string' ? value : ''

// A message longer than either full limit opens as a preview of at most
// previewLines lines and previewChars characters, cut on a line boundary.
export const deliveryPreviewLimits = { fullLines: 8, fullChars: 700, previewLines: 5, previewChars: 420 } as const

export type DeliveryPreview = { text: string, hiddenLines: number }

export type DeliveryPresentation = {
  sender: string
  recipient: string
  intent: string
  thread: string
  messageId: string
  operator: boolean
  body: string
  preview: DeliveryPreview | null
}

type Fence = { char: string, length: number }

const fenceOpener = /^ {0,3}(`{3,}|~{3,})(.*)$/

function openingFence(line: string): Fence | null {
  const match = fenceOpener.exec(line)
  if (!match) return null
  const [, run, info] = match
  if (run[0] === '`' && info.includes('`')) return null
  return { char: run[0], length: run.length }
}

function closesFence(line: string, fence: Fence) {
  const match = /^ {0,3}(`{3,}|~{3,})\s*$/.exec(line)
  return !!match && match[1][0] === fence.char && match[1].length >= fence.length
}

// openFenceAfter returns the code fence still open after these lines, if any.
function openFenceAfter(lines: string[]) {
  let open: Fence | null = null
  for (const line of lines) {
    if (open) { if (closesFence(line, open)) open = null }
    else open = openingFence(line)
  }
  return open
}

function cutAtWord(line: string, limit: number) {
  const slice = line.slice(0, limit)
  const space = slice.search(/\s\S*$/)
  return `${(space > limit / 2 ? slice.slice(0, space) : slice).trimEnd()}…`
}

// deliveryPreview trims a long agent message to a short markdown preview.
// Short messages return null and show in full with no expand control. A
// preview that stops inside a code fence gets the fence closed so the rest
// of the card does not render as code.
export function deliveryPreview(body: string): DeliveryPreview | null {
  const { fullLines, fullChars, previewLines, previewChars } = deliveryPreviewLimits
  const lines = body.split(/\r?\n/)
  if (lines.length <= fullLines && body.length <= fullChars) return null

  const kept: string[] = []
  let used = 0
  for (const line of lines) {
    if (kept.length >= previewLines) break
    if (used + line.length > previewChars) {
      if (kept.length === 0) kept.push(cutAtWord(line, previewChars))
      break
    }
    kept.push(line)
    used += line.length + 1
  }
  while (kept.length > 1 && !kept[kept.length - 1].trim()) kept.pop()
  const hiddenLines = lines.slice(kept.length).filter((line) => line.trim()).length
  const open = openFenceAfter(kept)
  if (open) kept.push(open.char.repeat(open.length))
  return { text: kept.join('\n'), hiddenLines }
}

// Web operator notes are the owner typing through herder web; they always
// show in full, like a prompt typed in the composer.
export function hcomDeliveryPresentation(delivery: DeliveryValue): DeliveryPresentation {
  const raw = text(delivery.text)
  const operator = isWebOperatorMessage(raw)
  const body = polishHcomDeliveryText(raw)
  return {
    sender: text(delivery.sender),
    recipient: text(delivery.recipient),
    intent: text(delivery.intent),
    thread: text(delivery.thread),
    messageId: text(delivery.message_id),
    operator,
    body,
    preview: operator ? null : deliveryPreview(body),
  }
}

export function deliveryExpandLabel(preview: DeliveryPreview, expanded: boolean) {
  if (expanded) return 'Show less'
  return preview.hiddenLines > 0 ? `Show full message · ${preview.hiddenLines} more ${preview.hiddenLines === 1 ? 'line' : 'lines'}` : 'Show full message'
}
