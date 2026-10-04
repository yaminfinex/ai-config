import type { QueuedMessage, TranscriptEntry } from '../../types'
import { objectValue } from '../../messagePolish.ts'

export function visibleQueuedMessages(queued: QueuedMessage[], entries: TranscriptEntry[]) {
  const delivered = new Set<string>()
  entries.forEach((entry) => {
    if (entry.kind !== 'hcom_delivery') return
    const deliveries = objectValue(entry.payload).deliveries
    if (!Array.isArray(deliveries)) return
    deliveries.forEach((delivery) => {
      const id = objectValue(delivery).message_id
      if (typeof id === 'string' && id) delivered.add(id)
    })
  })
  return queued.filter((message) => !delivered.has(String(message.id)))
}
