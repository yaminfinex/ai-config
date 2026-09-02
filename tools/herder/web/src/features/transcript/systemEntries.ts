import type { TranscriptEntry } from '../../types.ts'
import { objectValue, valueText, type ObjectValue } from './cleanRows.ts'

export type SystemEntryPresentation = {
  subtype: 'relocated' | 'model_refusal_fallback'
  summary: string
  detail: string
  alwaysVisible: boolean
}

function modelName(value: unknown) {
  const raw = valueText(value).replace(/^claude-/, '').replace(/\[[^\]]*\]$/, '')
  if (!raw) return ''
  return raw
    .replace(/-(\d+)-(\d+)$/, ' $1.$2')
    .split('-')
    .map((part) => part ? part[0].toUpperCase() + part.slice(1) : '')
    .join(' ')
}

export function systemEntryPresentation(value: unknown): SystemEntryPresentation | null {
  const payload = objectValue(value)
  if (valueText(payload.type) === 'relocated') {
    return {
      subtype: 'relocated',
      summary: `session moved to ${valueText(payload.relocatedCwd) || 'an unknown location'}`,
      detail: '',
      alwaysVisible: false,
    }
  }
  if (valueText(payload.type) !== 'system' || valueText(payload.subtype) !== 'model_refusal_fallback') return null
  const fallback = modelName(payload.fallbackModel)
  return {
    subtype: 'model_refusal_fallback',
    summary: `model switched${fallback ? ` to ${fallback}` : ''} — safeguards flagged a message`,
    detail: valueText(payload.content),
    alwaysVisible: true,
  }
}

function originalEntryType(payload: ObjectValue) {
  const type = valueText(payload.type)
  const subtype = valueText(payload.subtype)
  if (type && subtype) return `${type}/${subtype}`
  return type || subtype
}

export function unknownEntryLabel(entry: TranscriptEntry) {
  return `unknown entry · ${originalEntryType(objectValue(entry.payload)) || entry.kind}`
}
