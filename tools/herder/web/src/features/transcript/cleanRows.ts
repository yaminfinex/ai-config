import { isWebOperatorMessage } from '../../messagePolish.ts'
import type { TranscriptEntry } from '../../types.ts'
import {
  activityPillTone,
  cleanViewDisposition,
  isCleanConversationDelivery,
  markerOnlyAssistantActivity,
  type ActivityAggregation,
  type ActivityPillTone,
} from './cleanView.ts'
import { systemEntryPresentation } from './systemEntries.ts'

export type ObjectValue = Record<string, unknown>

export const objectValue = (value: unknown): ObjectValue => value && typeof value === 'object' && !Array.isArray(value) ? value as ObjectValue : {}
export const valueText = (value: unknown) => typeof value === 'string' ? value : typeof value === 'number' || typeof value === 'boolean' ? String(value) : ''

export function messageText(payload: unknown): string {
  const value = objectValue(payload)
  const content = objectValue(value.message).content ?? value.content
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content.map((block) => {
    const part = objectValue(block)
    return valueText(part.text ?? part.thinking)
  }).filter(Boolean).join('\n')
}

export type CleanActivity = {
  key: string
  label: string
  tone: ActivityPillTone
  title?: string
  aggregation?: ActivityAggregation
  entry: TranscriptEntry
  index: number
  deliveryIndex?: number
}

export type CleanRow =
  | { type: 'entry', key: string, entry: TranscriptEntry, index: number, deliveryIndex?: number }
  | { type: 'run', key: string, activities: CleanActivity[] }

export type CleanRowRelationships = {
  pairedToolResults: Set<number>
  pairedDeliveries: Set<number>
  pairedCommandOutputs: Set<number>
  duplicateHcomDeliveries: Map<number, Set<number>>
}

function activityLabel(entry: TranscriptEntry) {
  const payload = objectValue(entry.payload)
  if (entry.kind === 'tool_use') return valueText(payload.name) || 'tool'
  if (entry.kind === 'tool_result') return 'tool result'
  if (entry.kind === 'thinking') return 'thinking'
  if (entry.kind === 'command_stdout') {
    return messageText(entry.payload).match(/<command-name>([\s\S]*?)<\/command-name>/)?.[1] || 'command'
  }
  if (entry.kind === 'task_notification') return 'task'
  return entry.quarantine ? 'quarantined' : 'unknown'
}

export function cleanRows(entries: TranscriptEntry[], relationships: CleanRowRelationships): CleanRow[] {
  const rows: CleanRow[] = []
  let run: CleanActivity[] = []
  const flush = () => {
    if (run.length === 0) return
    rows.push({ type: 'run', key: `run:${run[0].key}`, activities: run })
    run = []
  }
  const addEntry = (entry: TranscriptEntry, index: number, deliveryIndex?: number) => {
    flush()
    rows.push({ type: 'entry', key: deliveryIndex == null ? `entry:${entry.uuid ?? entry.byteOffset}` : `delivery:${entry.uuid ?? entry.byteOffset}:${deliveryIndex}`, entry, index, deliveryIndex })
  }

  entries.forEach((entry, index) => {
    if (relationships.pairedToolResults.has(index) || relationships.pairedCommandOutputs.has(index) || relationships.pairedDeliveries.has(index)) return
    if (entry.kind === 'assistant_text') {
      const content = messageText(entry.payload)
      const markerActivity = /^\s*<(?:internal|status)>/.test(content) ? markerOnlyAssistantActivity(content) : null
      if (markerActivity) {
        run.push({ key: `activity:${entry.uuid ?? entry.byteOffset}`, ...markerActivity, entry, index })
        return
      }
    }
    const disposition = cleanViewDisposition[entry.kind]
    if (disposition === 'delivery') {
      const delivery = entry.kind === 'hcom_delivery_stub' ? entries[index + 1] : entry
      const deliveryIndex = entry.kind === 'hcom_delivery_stub' ? index + 1 : index
      if (delivery?.kind !== 'hcom_delivery') return
      const values = objectValue(delivery.payload).deliveries
      if (!Array.isArray(values)) return
      const duplicates = relationships.duplicateHcomDeliveries.get(deliveryIndex)
      values.forEach((raw, valueIndex) => {
        const message = objectValue(raw)
        if (duplicates?.has(valueIndex) || !isCleanConversationDelivery(message)) return
        if (isWebOperatorMessage(valueText(message.text))) {
          addEntry(delivery, deliveryIndex, valueIndex)
          return
        }
        run.push({
          key: `message:${delivery.uuid ?? delivery.byteOffset}:${valueIndex}`,
          label: `✉ ${valueText(message.sender) || 'unknown sender'}`,
          tone: activityPillTone(delivery.kind, true),
          entry: delivery,
          index: deliveryIndex,
          deliveryIndex: valueIndex,
        })
      })
      return
    }
    if (disposition === 'activity') {
      const label = activityLabel(entry)
      run.push({ key: `activity:${entry.uuid ?? entry.byteOffset}`, label, tone: activityPillTone(entry.kind), aggregation: entry.kind === 'tool_use' ? { category: 'tool', content: label } : undefined, entry, index })
      return
    }
    if (disposition === 'system') {
      if (systemEntryPresentation(entry.payload)?.alwaysVisible) addEntry(entry, index)
      return
    }
    if (disposition === 'show') addEntry(entry, index)
  })
  flush()
  return rows
}
