type ObjectValue = Record<string, unknown>

type HcomEntry = {
  kind: string
  payload: unknown
}

export const webOperatorNoteStart = '[HERDER_WEB_OPERATOR_NOTE_BEGIN]'
export const webOperatorNoteEnd = '[HERDER_WEB_OPERATOR_NOTE_END]'

const legacyWebOperatorStart = '[This message came from a web operator named '
const legacyWebOperatorEnd = ' via the fleet web view. They cannot receive hcom messages; do not reply with `hcom send`. Answer in your normal chat turn; they are watching the session transcript live.]'
const prereleaseWebOperatorStart = '<<<HERDER_WEB_OPERATOR_NOTE>>>'
const prereleaseWebOperatorEnd = '<<<END_HERDER_WEB_OPERATOR_NOTE>>>'

function objectValue(value: unknown): ObjectValue {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as ObjectValue : {}
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function removeFollowingBlankLine(text: string): string {
  return text.startsWith('\r\n\r\n') ? text.slice(4)
    : text.startsWith('\n\n') ? text.slice(2)
      : text
}

function stripFencedPrefix(text: string, start: string, end: string): string | null {
  if (!text.startsWith(start + '\n')) return null
  const blockEnd = text.indexOf('\n' + end, start.length + 1)
  if (blockEnd === -1) return null
  return removeFollowingBlankLine(text.slice(blockEnd + end.length + 1))
}

export function isWebOperatorMessage(text: string): boolean {
  if (stripFencedPrefix(text, webOperatorNoteStart, webOperatorNoteEnd) !== null) return true
  if (stripFencedPrefix(text, prereleaseWebOperatorStart, prereleaseWebOperatorEnd) !== null) return true
  if (!text.startsWith(legacyWebOperatorStart)) return false
  const noteEnd = text.indexOf(legacyWebOperatorEnd, legacyWebOperatorStart.length)
  if (noteEnd === -1) return false
  const sender = text.slice(legacyWebOperatorStart.length, noteEnd)
  return Boolean(sender && !sender.includes('\n') && !sender.includes('\r'))
}

// stripWebOperatorNote recognizes only exact known prefixes: the current
// fenced block, its pre-release marker spelling, and the legacy sentence
// already stored in session files. Similar prose elsewhere remains untouched.
export function stripWebOperatorNote(text: string): string {
  const fenced = stripFencedPrefix(text, webOperatorNoteStart, webOperatorNoteEnd)
    ?? stripFencedPrefix(text, prereleaseWebOperatorStart, prereleaseWebOperatorEnd)
  if (fenced !== null) return fenced

  if (!text.startsWith(legacyWebOperatorStart)) return text
  const noteEnd = text.indexOf(legacyWebOperatorEnd, legacyWebOperatorStart.length)
  if (noteEnd === -1) return text
  const sender = text.slice(legacyWebOperatorStart.length, noteEnd)
  if (!sender || sender.includes('\n') || sender.includes('\r')) return text
  return removeFollowingBlankLine(text.slice(noteEnd + legacyWebOperatorEnd.length))
}

function deliveries(entry: HcomEntry): unknown[] {
  const value = objectValue(entry.payload).deliveries
  return Array.isArray(value) ? value : []
}

function sameBusDelivery(left: unknown, right: unknown): boolean {
  const leftDelivery = objectValue(left)
  const rightDelivery = objectValue(right)
  const leftID = stringValue(leftDelivery.message_id)
  const rightID = stringValue(rightDelivery.message_id)
  if (leftID || rightID) return Boolean(leftID && rightID && leftID === rightID)
  const leftBody = stringValue(leftDelivery.text)
  return Boolean(leftBody && leftBody === stringValue(rightDelivery.text))
}

// duplicateHcomDeliveryIndices is a pure view relationship. It marks only
// cards repeated in the immediately following hcom attachment; entries remain
// present, ordered, and unchanged in the source window.
export function duplicateHcomDeliveryIndices(entries: HcomEntry[]): Map<number, Set<number>> {
  const duplicates = new Map<number, Set<number>>()
  entries.forEach((entry, entryIndex) => {
    const previous = entries[entryIndex - 1]
    if (entry.kind !== 'hcom_delivery' || previous?.kind !== 'hcom_delivery') return
    const previousDeliveries = deliveries(previous)
    deliveries(entry).forEach((delivery, deliveryIndex) => {
      if (!previousDeliveries.some((candidate) => sameBusDelivery(candidate, delivery))) return
      const indices = duplicates.get(entryIndex) ?? new Set<number>()
      indices.add(deliveryIndex)
      duplicates.set(entryIndex, indices)
    })
  })
  return duplicates
}
