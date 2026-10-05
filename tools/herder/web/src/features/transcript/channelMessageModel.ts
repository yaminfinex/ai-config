import { deliveryPreview, type DeliveryPreview } from './deliveryModel.ts'

type ObjectValue = Record<string, unknown>

const text = (value: unknown) => typeof value === 'string' ? value : ''
const object = (value: unknown): ObjectValue => value && typeof value === 'object' && !Array.isArray(value) ? value as ObjectValue : {}

// A channel message is someone outside the fleet writing in through a
// Claude channel plugin. The serve carries no avatar URL, so the card shows
// initials and never fetches anything the transcript names.
export type ChannelMessagePresentation =
  | { kind: 'message', key: string, sender: string, initials: string, senderKind: string, source: string, sourceLabel: string, messageId: string, createdAt: string, body: string, preview: DeliveryPreview | null }
  | { kind: 'raw', key: string, sourceLabel: string, body: string }

export function channelSenderName(message: ObjectValue): string {
  const sender = object(message.sender)
  return text(sender.name).trim() || text(sender.login).trim() || text(message.sender_raw).trim() || 'unknown sender'
}

export function channelInitials(name: string): string {
  const words = name.replace(/[^\p{L}\p{N}]+/gu, ' ').trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return '?'
  const letters = words.length === 1 ? [...words[0]].slice(0, 2) : [[...words[0]][0], [...words[words.length - 1]][0]]
  return letters.join('').toUpperCase()
}

// channelMessagePresentation turns one channel_message payload into its
// cards in order. Text outside a <channel> block, or a payload the serve did
// not split, still shows as a raw card: nothing the agent saw is dropped.
export function channelMessagePresentation(payload: unknown): ChannelMessagePresentation[] {
  const value = object(payload)
  const fallbackLabel = text(value.source_label) || 'channel'
  const messages = Array.isArray(value.messages) ? value.messages : []
  return messages.flatMap((raw, index): ChannelMessagePresentation[] => {
    const message = object(raw)
    const body = text(message.text)
    if (message.raw === true) return body ? [{ kind: 'raw', key: `raw:${index}`, sourceLabel: fallbackLabel, body }] : []
    const sender = channelSenderName(message)
    return [{
      kind: 'message',
      key: text(message.message_id) || `message:${index}`,
      sender,
      initials: channelInitials(sender),
      senderKind: text(object(message.sender).kind),
      source: text(message.source),
      sourceLabel: text(message.source_label) || fallbackLabel,
      messageId: text(message.message_id),
      createdAt: text(message.created_at),
      body,
      preview: deliveryPreview(body),
    }]
  })
}
