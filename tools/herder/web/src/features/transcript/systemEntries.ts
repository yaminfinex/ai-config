import type { TranscriptEntry } from '../../types.ts'

type ObjectValue = Record<string, unknown>

const objectValue = (value: unknown): ObjectValue => value && typeof value === 'object' && !Array.isArray(value) ? value as ObjectValue : {}
const valueText = (value: unknown) => typeof value === 'string' ? value : typeof value === 'number' || typeof value === 'boolean' ? String(value) : ''

export type SystemEntryPresentation = {
  subtype: 'relocated' | 'model_refusal_fallback' | 'model_consent_fallback'
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
  if (valueText(payload.type) !== 'system') return null
  const subtype = valueText(payload.subtype)
  if (subtype !== 'model_refusal_fallback' && subtype !== 'model_consent_fallback') return null
  const fallback = modelName(payload.fallbackModel)
  return {
    subtype,
    summary: `model switched${fallback ? ` to ${fallback}` : ''} — ${subtype === 'model_refusal_fallback' ? 'safeguards flagged a message' : 'consent required'}`,
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
