import type { QueuedMessage } from '../../types'

function queuedTime(timestamp: string, now: number) {
  const sent = Date.parse(timestamp)
  if (!Number.isFinite(sent)) return timestamp
  const seconds = Math.max(0, Math.floor((now - sent) / 1000))
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  return minutes < 60 ? `${minutes}m ago` : new Date(sent).toLocaleTimeString()
}

export function QueuedMessages({ messages, now }: { messages: QueuedMessage[], now: number }) {
  if (messages.length === 0) return null
  return <section className="queued-messages" aria-label="Queued messages">
    <header><strong>queued</strong><span>waiting for the agent’s next turn</span></header>
    {messages.map((message) => {
      const operator = Boolean(message.operator)
      return <article className={`queued-message${operator ? ' operator-queued' : ''}`} key={message.id}>
        <div className="queued-meta"><strong>{message.sender}</strong>{operator && <span className="operator-badge">operator</span>}{message.intent && <span className={`intent-badge ${message.intent}`}>{message.intent}</span>}<span className="message-id">#{message.id}</span><time dateTime={message.sent_at}>{queuedTime(message.sent_at, now)}</time></div>
        <div className="queued-preview">{message.preview}</div>
      </article>
    })}
  </section>
}
