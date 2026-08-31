import type { EntryKind } from '../../types'
import { parseAssistantFencing } from './fencingModel.ts'

export type CleanViewDisposition = 'show' | 'delivery' | 'activity' | 'system' | 'hide'

// This exhaustive policy mirrors the serializer's shared Claude/Codex taxonomy.
// Adding an EntryKind fails typecheck until its clean-view behavior is chosen.
export const cleanViewDisposition = {
  human_prompt: 'show', // Direct operator conversation.
  hcom_delivery_stub: 'delivery', // Carrier for a following parsed hcom conversation delivery.
  hcom_delivery: 'delivery', // Parsed hcom cards are classified individually below.
  task_notification: 'activity', // Background work is compact progress.
  injected_system: 'system', // Governed by the system-entries preference.
  command_stdout: 'activity', // Slash-command activity joins a compact run.
  compact_divider: 'show', // A real conversation epoch boundary.
  assistant_text: 'show', // The agent's visible markdown reply.
  thinking: 'activity', // Private reasoning is visible only as progress.
  tool_use: 'activity', // Tool machinery joins a compact run.
  tool_result: 'activity', // Unpaired results remain honest compact activity.
  turn_duration: 'hide', // Turn telemetry.
  system_chip: 'system', // Governed by the system-entries preference.
  unknown: 'activity', // Unclassified machinery gets an honest generic pill.
} as const satisfies Record<EntryKind, CleanViewDisposition>

type DeliveryValue = Record<string, unknown>
type CleanViewStorage = Pick<Storage, 'getItem' | 'setItem'>
export type TranscriptViewMode = 'compact' | 'normal' | 'full'
export type ActivityPillTone = 'tool' | 'thinking' | 'message' | 'other' | 'assistant-status'
export type ActivityAggregation = { category: string, content: string }

const legacyCleanViewPreferencePrefix = 'herder.web.cleanView.v1:'
const legacyShowSystemPreferencePrefix = 'herder.web.showSystem.v1:'
export const transcriptViewPreferencePrefix = 'herder.web.transcriptView.v1:'

export function transcriptViewPreferenceKey(agentName: string) {
  return `${transcriptViewPreferencePrefix}${encodeURIComponent(agentName)}`
}

export function isCleanConversationDelivery(delivery: DeliveryValue): boolean {
  // These exact wire fields identify bus lifecycle traffic without inspecting
  // or guessing from the rendered message body.
  return delivery.sender !== '[hcom-launcher]' && delivery.intent !== 'ack'
}

export function aggregateActivityPills<T extends { key: string, label: string, aggregation?: ActivityAggregation }>(activities: T[]) {
  const pills: Array<T & { count: number }> = []
  activities.forEach((activity) => {
    const previous = pills[pills.length - 1]
    if (activity.aggregation && previous?.aggregation?.category === activity.aggregation.category && previous.aggregation.content === activity.aggregation.content) {
      previous.count++
      return
    }
    pills.push({ ...activity, count: 1 })
  })
  return pills
}

export function markerOnlyAssistantActivity(content: string) {
  const fencing = parseAssistantFencing(content)
  if (!fencing.fenced || fencing.hasVisibleText) return null
  const markers = fencing.segments.filter((segment) => segment.kind !== 'text')
  if (markers.length === 0) return null

  const statuses = markers.filter((segment) => segment.kind === 'status')
  const hasInternal = markers.some((segment) => segment.kind === 'internal')
  const category = statuses.length > 0 ? hasInternal ? 'mixed' : 'status' : 'internal'
  const label = [
    ...statuses.map((segment) => segment.content.trim() ? segment.content : 'status'),
    ...(hasInternal ? ['internal note'] : []),
  ].join(' · ')
  const title = markers.map((segment) => segment.content.trim()).filter(Boolean).join('\n') || label

  return {
    label,
    tone: statuses.length > 0 ? 'assistant-status' as const : 'thinking' as const,
    title,
    aggregation: { category: `assistant-${category}`, content },
  }
}

export function splitFinalActivityRun<T>(rows: readonly { type: string, activities?: readonly T[] }[]) {
  const finalRow = rows[rows.length - 1]
  if (finalRow?.type !== 'run' || !finalRow.activities?.length) return null
  return {
    collapsed: finalRow.activities.slice(0, -1),
    latest: finalRow.activities[finalRow.activities.length - 1],
  }
}

export function approximateActivityAge(timestamp: string | undefined, now: number) {
  if (!timestamp) return 'time unknown'
  const delta = now - Date.parse(timestamp)
  if (!Number.isFinite(delta)) return 'time unknown'
  const elapsed = Math.max(0, delta)
  if (elapsed < 60_000) return `${Math.max(1, Math.round(elapsed / 1000))}s`
  if (elapsed < 3_600_000) return `${Math.round(elapsed / 60_000)}m`
  if (elapsed < 86_400_000) return `${Math.round(elapsed / 3_600_000)}h`
  return `${Math.round(elapsed / 86_400_000)}d`
}

export function activityPillTone(kind: EntryKind, busMessage = false): ActivityPillTone {
  if (busMessage) return 'message'
  if (kind === 'tool_use' || kind === 'tool_result' || kind === 'command_stdout') return 'tool'
  if (kind === 'thinking') return 'thinking'
  return 'other'
}

export function readTranscriptViewMode(agentName: string, storage: CleanViewStorage | null = browserStorage()): TranscriptViewMode {
  if (!storage) return 'compact'
  const encodedName = encodeURIComponent(agentName)
  try {
    const stored = storage.getItem(transcriptViewPreferenceKey(agentName))
    if (stored === 'compact' || stored === 'normal' || stored === 'full') return stored
    const cleanView = storage.getItem(`${legacyCleanViewPreferencePrefix}${encodedName}`) === 'true'
    const showSystem = storage.getItem(`${legacyShowSystemPreferencePrefix}${encodedName}`) === 'true'
    const migrated = cleanView ? 'compact' : showSystem ? 'full' : 'compact'
    try { storage.setItem(transcriptViewPreferenceKey(agentName), migrated) } catch { /* best-effort migration */ }
    return migrated
  } catch {
    return 'compact'
  }
}

export function persistTranscriptViewMode(agentName: string, mode: TranscriptViewMode, storage: Pick<CleanViewStorage, 'setItem'> | null = browserStorage()) {
  try {
    storage?.setItem(transcriptViewPreferenceKey(agentName), mode)
  } catch {
    // View persistence is best-effort when browser storage is unavailable.
  }
}

function browserStorage(): CleanViewStorage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}
