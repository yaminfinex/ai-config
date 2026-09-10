import { createElement } from 'react'

import { tokenCount } from '../../shared/agentVitals.ts'

export function ContextUsed({ value }: { value?: number }) {
  return value === undefined ? null : createElement('span', { className: 'context-used' }, tokenCount(value))
}

export function contextUsedTooltip(value?: number) {
  return value === undefined ? '' : ` · context ${value.toLocaleString('en-US')} tokens`
}
