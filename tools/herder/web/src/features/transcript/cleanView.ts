import type { EntryKind } from '../../types'

export type CleanViewDisposition = 'show' | 'delivery' | 'hide'

// This exhaustive policy mirrors the serializer's shared Claude/Codex taxonomy.
// Adding an EntryKind fails typecheck until its clean-view behavior is chosen.
export const cleanViewDisposition = {
  human_prompt: 'show', // Direct operator conversation.
  hcom_delivery_stub: 'delivery', // Carrier for a following parsed hcom conversation delivery.
  hcom_delivery: 'delivery', // Parsed hcom cards are classified individually below.
  task_notification: 'hide', // Background-task machinery, not a chat turn.
  injected_system: 'hide', // Prompt scaffolding injected by the harness.
  command_stdout: 'hide', // Slash-command invocation and local output.
  compact_divider: 'show', // A real conversation epoch boundary.
  assistant_text: 'show', // The agent's visible markdown reply.
  thinking: 'hide', // Private reasoning rather than conversation.
  tool_use: 'hide', // Tool machinery.
  tool_result: 'hide', // Tool machinery.
  turn_duration: 'hide', // Turn telemetry.
  system_chip: 'hide', // Scheduled hooks, summaries, and other system noise.
  unknown: 'hide', // Unclassified/quarantined machinery is not presumed to be chat.
} as const satisfies Record<EntryKind, CleanViewDisposition>

type DeliveryValue = Record<string, unknown>
type CleanViewStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export const cleanViewPreferencePrefix = 'herder.web.cleanView.v1:'

export function cleanViewPreferenceKey(agentName: string) {
  return `${cleanViewPreferencePrefix}${encodeURIComponent(agentName)}`
}

export function isCleanConversationDelivery(delivery: DeliveryValue): boolean {
  // These exact wire fields identify bus lifecycle traffic without inspecting
  // or guessing from the rendered message body.
  return delivery.sender !== '[hcom-launcher]' && delivery.intent !== 'ack'
}

export function readCleanView(agentName: string, storage: Pick<CleanViewStorage, 'getItem'> | null = browserStorage()) {
  try {
    return storage?.getItem(cleanViewPreferenceKey(agentName)) === 'true'
  } catch {
    return false
  }
}

export function persistCleanView(agentName: string, enabled: boolean, storage: Pick<CleanViewStorage, 'setItem' | 'removeItem'> | null = browserStorage()) {
  try {
    if (!storage) return
    if (enabled) storage.setItem(cleanViewPreferenceKey(agentName), 'true')
    else storage.removeItem(cleanViewPreferenceKey(agentName))
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
