import type { EntryKind } from '../../types'

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
type CleanViewStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export const cleanViewPreferencePrefix = 'herder.web.cleanView.v1:'
export const showSystemPreferencePrefix = 'herder.web.showSystem.v1:'

export function cleanViewPreferenceKey(agentName: string) {
  return `${cleanViewPreferencePrefix}${encodeURIComponent(agentName)}`
}

export function showSystemPreferenceKey(agentName: string) {
  return `${showSystemPreferencePrefix}${encodeURIComponent(agentName)}`
}

export function isCleanConversationDelivery(delivery: DeliveryValue): boolean {
  // These exact wire fields identify bus lifecycle traffic without inspecting
  // or guessing from the rendered message body.
  return delivery.sender !== '[hcom-launcher]' && delivery.intent !== 'ack'
}

export function aggregateActivityPills<T extends { key: string, label: string }>(activities: T[], canAggregate: (activity: T) => boolean) {
  const pills: { key: string, label: string, count: number }[] = []
  activities.forEach((activity) => {
    const previous = pills[pills.length - 1]
    if (canAggregate(activity) && previous?.label === activity.label) {
      previous.count++
      return
    }
    pills.push({ key: activity.key, label: activity.label, count: 1 })
  })
  return pills
}

export function readCleanView(agentName: string, storage: Pick<CleanViewStorage, 'getItem'> | null = browserStorage()) {
  return readPreference(cleanViewPreferenceKey(agentName), storage)
}

export function persistCleanView(agentName: string, enabled: boolean, storage: Pick<CleanViewStorage, 'setItem' | 'removeItem'> | null = browserStorage()) {
  persistPreference(cleanViewPreferenceKey(agentName), enabled, storage)
}

export function readShowSystem(agentName: string, storage: Pick<CleanViewStorage, 'getItem'> | null = browserStorage()) {
  return readPreference(showSystemPreferenceKey(agentName), storage)
}

export function persistShowSystem(agentName: string, enabled: boolean, storage: Pick<CleanViewStorage, 'setItem' | 'removeItem'> | null = browserStorage()) {
  persistPreference(showSystemPreferenceKey(agentName), enabled, storage)
}

function readPreference(key: string, storage: Pick<CleanViewStorage, 'getItem'> | null) {
  try {
    return storage?.getItem(key) === 'true'
  } catch {
    return false
  }
}

function persistPreference(key: string, enabled: boolean, storage: Pick<CleanViewStorage, 'setItem' | 'removeItem'> | null) {
  try {
    if (!storage) return
    if (enabled) storage.setItem(key, 'true')
    else storage.removeItem(key)
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
