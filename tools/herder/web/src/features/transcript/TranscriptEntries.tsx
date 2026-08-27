import { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { duplicateHcomDeliveryIndices, stripWebOperatorNote } from '../../messagePolish'
import type { TranscriptEntry } from '../../types'

type ObjectValue = Record<string, unknown>
const objectValue = (value: unknown): ObjectValue => value && typeof value === 'object' && !Array.isArray(value) ? value as ObjectValue : {}
const valueText = (value: unknown) => typeof value === 'string' ? value : typeof value === 'number' || typeof value === 'boolean' ? String(value) : ''

function messageText(payload: unknown): string {
  const content = objectValue(objectValue(payload).message).content
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content.map((block) => {
    const part = objectValue(block)
    return valueText(part.text ?? part.thinking)
  }).filter(Boolean).join('\n')
}

function formatDuration(milliseconds: number) {
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '—'
  if (milliseconds < 1000) return `${Math.round(milliseconds)}ms`
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)}s`
  return `${Math.floor(milliseconds / 60_000)}m ${Math.round(milliseconds % 60_000 / 1000)}s`
}

function relativeTime(timestamp: string | undefined, now: number) {
  if (!timestamp) return 'time unknown'
  const delta = now - Date.parse(timestamp)
  if (!Number.isFinite(delta)) return timestamp
  const future = delta < 0
  const elapsed = Math.abs(delta)
  const amount = elapsed < 60_000 ? Math.max(1, Math.round(elapsed / 1000))
    : elapsed < 3_600_000 ? Math.round(elapsed / 60_000)
      : elapsed < 86_400_000 ? Math.round(elapsed / 3_600_000)
        : Math.round(elapsed / 86_400_000)
  const unit = elapsed < 60_000 ? 's' : elapsed < 3_600_000 ? 'm' : elapsed < 86_400_000 ? 'h' : 'd'
  return future ? `in ${amount}${unit}` : `${amount}${unit} ago`
}

function Timestamp({ timestamp, now, absolute = false }: { timestamp?: string, now: number, absolute?: boolean }) {
  if (!timestamp) return <span className="entry-time" title="Timestamp unavailable">time unknown</span>
  const absoluteText = new Date(timestamp).toLocaleString()
  return <time className="entry-time" dateTime={timestamp} title={absoluteText}>{absolute ? `${absoluteText} · ` : ''}{relativeTime(timestamp, now)}</time>
}

function toolSummary(name: string, input: ObjectValue) {
  const preferred = name === 'Bash' ? input.command : ['Edit', 'Write', 'Read'].includes(name) ? (input.file_path ?? input.path ?? input.file) : undefined
  if (valueText(preferred)) return valueText(preferred).replace(/\s+/g, ' ')
  for (const value of Object.values(input)) {
    const text = valueText(value).replace(/\s+/g, ' ')
    if (text) return text
  }
  return 'no input summary'
}

function resultText(content: unknown) {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return content == null ? '' : JSON.stringify(content, null, 2)
  return content.map((block) => {
    const item = objectValue(block)
    if (item.type === 'image') return '[image result]'
    return valueText(item.text ?? item.raw)
  }).filter(Boolean).join('\n')
}

function structuredPatch(result: ObjectValue) {
  const patches = objectValue(result.toolUseResult).structuredPatch
  return (Array.isArray(patches) ? patches : []).flatMap((patch) => {
    const lines = objectValue(patch).lines
    return Array.isArray(lines) ? lines.filter((line): line is string => typeof line === 'string') : []
  })
}

function ToolEntry({ entry, result, now }: { entry: TranscriptEntry, result?: TranscriptEntry, now: number }) {
  const call = objectValue(entry.payload)
  const outcome = objectValue(result?.payload)
  const name = valueText(call.name) || 'unknown tool'
  const input = objectValue(call.input)
  const duration = result ? formatDuration(Date.parse(result.timestamp ?? '') - Date.parse(entry.timestamp ?? '')) : 'running · no result yet'
  const patch = structuredPatch(outcome)
  const output = resultText(outcome.content)
  const imageCount = Number(outcome.image_count ?? 0)
  return <details className="entry-expander tool-entry"><summary>
    <span className={`tool-status ${result ? outcome.is_error === true ? 'error' : 'success' : 'running'}`} />
    <strong>{name}</strong><span className="tool-summary">{toolSummary(name, input)}</span>
    <span className="tool-duration">{duration}</span><Timestamp timestamp={result?.timestamp ?? entry.timestamp} now={now} />
  </summary><div className="entry-detail">
    <h4>Input</h4><pre>{JSON.stringify(input, null, 2)}</pre>
    {result && <><h4>Output</h4>{output && <pre>{output}</pre>}
      {imageCount > 0 && <div className="image-placeholder">▧ {imageCount} image result{imageCount === 1 ? '' : 's'} present (not served)</div>}
      {outcome.truncated === true && <div className="truncation-banner">Output capped at 16 KiB — {Number(outcome.total_bytes).toLocaleString()} bytes total.</div>}
      {patch.length > 0 && <><h4>Structured patch</h4><pre className="diff-output">{patch.map((line, index) => <span className={line.startsWith('+') ? 'diff-add' : line.startsWith('-') ? 'diff-delete' : ''} key={`${index}:${line}`}>{line}{'\n'}</span>)}</pre></>}
    </>}
  </div></details>
}

type EntryRelationships = {
  toolResults: Map<string, TranscriptEntry>
  pairedToolResults: Set<number>
  pairedDeliveries: Set<number>
  commandOutputs: Map<number, TranscriptEntry>
  pairedCommandOutputs: Set<number>
  duplicateHcomDeliveries: Map<number, Set<number>>
}

function relateEntries(entries: TranscriptEntry[]): EntryRelationships {
  const toolResults = new Map<string, TranscriptEntry>()
  const toolUses = new Set<string>()
  entries.forEach((entry) => {
    const id = valueText(objectValue(entry.payload).tool_use_id)
    if (entry.kind === 'tool_result' && id) toolResults.set(id, entry)
    if (entry.kind === 'tool_use' && id) toolUses.add(id)
  })
  const pairedToolResults = new Set(entries.flatMap((entry, index) => entry.kind === 'tool_result' && toolUses.has(valueText(objectValue(entry.payload).tool_use_id)) ? [index] : []))
  const pairedDeliveries = new Set<number>()
  entries.forEach((entry, index) => { if (entry.kind === 'hcom_delivery_stub' && entries[index + 1]?.kind === 'hcom_delivery') pairedDeliveries.add(index + 1) })
  const commandOutputs = new Map<number, TranscriptEntry>()
  const pairedCommandOutputs = new Set<number>()
  entries.forEach((entry, index) => {
    const next = entries[index + 1]
    if (entry.kind === 'command_stdout' && messageText(entry.payload).includes('<command-name>') && next?.kind === 'command_stdout' && messageText(next.payload).includes('<local-command-stdout>')) {
      commandOutputs.set(index, next)
      pairedCommandOutputs.add(index + 1)
    }
  })
  return { toolResults, pairedToolResults, pairedDeliveries, commandOutputs, pairedCommandOutputs, duplicateHcomDeliveries: duplicateHcomDeliveryIndices(entries) }
}

function HcomCards({ entry, entryIndex, now, showSystem, relationships }: { entry: TranscriptEntry, entryIndex: number, now: number, showSystem: boolean, relationships: EntryRelationships }) {
  const deliveries = objectValue(entry.payload).deliveries
  const values = Array.isArray(deliveries) ? deliveries : []
  const duplicates = relationships.duplicateHcomDeliveries.get(entryIndex)
  const visibleValues = values.flatMap((raw, index) => duplicates?.has(index) ? [] : [{ raw, index }])
  if (visibleValues.length === 0) return null
  const parsed = visibleValues.some(({ raw }) => Boolean(valueText(objectValue(raw).sender) && valueText(objectValue(raw).message_id)))
  if (!parsed) return showSystem ? <details className="system-chip unknown-entry"><summary>unparsed hook attachment · <Timestamp timestamp={entry.timestamp} now={now} /></summary><pre>{visibleValues.map(({ raw }) => valueText(objectValue(raw).text)).filter(Boolean).join('\n')}</pre></details> : null
  return <>{visibleValues.map(({ raw, index }) => {
    const delivery = objectValue(raw)
    return <article className="entry-card hcom-card" key={`${entry.uuid ?? entry.byteOffset}:${index}`}>
      <header><strong>{valueText(delivery.sender) || 'unknown sender'}</strong><span>→ {valueText(delivery.recipient) || 'unknown recipient'}</span>
        {valueText(delivery.intent) && <span className={`intent-badge ${valueText(delivery.intent)}`}>{valueText(delivery.intent)}</span>}
        {valueText(delivery.message_id) && <span className="message-id">#{valueText(delivery.message_id)}</span>}
        {valueText(delivery.thread) && <span className="thread-chip">{valueText(delivery.thread)}</span>}
        <Timestamp timestamp={entry.timestamp} now={now} />
      </header><div>{stripWebOperatorNote(valueText(delivery.text)) || '(delivery body unavailable)'}</div>
    </article>
  })}</>
}

function EntryView({ entry, index, entries, relationships, agentName, now, showSystem }: { entry: TranscriptEntry, index: number, entries: TranscriptEntry[], relationships: EntryRelationships, agentName: string, now: number, showSystem: boolean }) {
  const payload = objectValue(entry.payload)
  const content = messageText(entry.payload)
  if (relationships.pairedToolResults.has(index) || relationships.pairedDeliveries.has(index) || relationships.pairedCommandOutputs.has(index)) return null
  if (entry.kind === 'hcom_delivery_stub') {
    const delivery = entries[index + 1]
    return delivery?.kind === 'hcom_delivery' ? <HcomCards entry={delivery} entryIndex={index + 1} now={now} showSystem={showSystem} relationships={relationships} /> : <div className="system-chip">hcom delivery pending attachment · <Timestamp timestamp={entry.timestamp} now={now} /></div>
  }
  if (entry.kind === 'hcom_delivery') return <HcomCards entry={entry} entryIndex={index} now={now} showSystem={showSystem} relationships={relationships} />
  if (entry.kind === 'tool_use') return <ToolEntry entry={entry} result={relationships.toolResults.get(valueText(payload.tool_use_id))} now={now} />
  if (entry.kind === 'tool_result') return <details className="entry-expander tool-entry"><summary><span className={`tool-status ${payload.is_error === true ? 'error' : 'success'}`} /><strong>unpaired tool result</strong><Timestamp timestamp={entry.timestamp} now={now} /></summary><div className="entry-detail"><pre>{resultText(payload.content)}</pre></div></details>
  if (entry.kind === 'assistant_text') return <article className="assistant-entry"><header><strong>{agentName}</strong><Timestamp timestamp={entry.timestamp} now={now} /></header><div className="markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown></div></article>
  if (entry.kind === 'thinking') {
    const nextTime = entries.slice(index + 1).find((candidate) => candidate.timestamp)?.timestamp
    const duration = nextTime && entry.timestamp ? formatDuration(Date.parse(nextTime) - Date.parse(entry.timestamp)) : 'duration unknown'
    return <details className="entry-expander thinking-entry"><summary>thinking · {duration}<Timestamp timestamp={entry.timestamp} now={now} /></summary><div className="entry-detail">{content || 'Thinking content unavailable.'}</div></details>
  }
  if (entry.kind === 'human_prompt') return <article className="entry-card human-entry"><header><strong>owner (terminal)</strong><Timestamp timestamp={entry.timestamp} now={now} absolute /></header><div>{content || 'Prompt body unavailable.'}</div></article>
  if (entry.kind === 'command_stdout') {
    const command = content.match(/<command-name>([\s\S]*?)<\/command-name>/)?.[1] ?? 'slash command'
    const args = content.match(/<command-args>([\s\S]*?)<\/command-args>/)?.[1] ?? ''
    const outputContent = messageText(relationships.commandOutputs.get(index)?.payload) || content
    const output = outputContent.match(/<local-command-stdout>([\s\S]*?)<\/local-command-stdout>/)?.[1] ?? ''
    return <details className="entry-expander command-entry"><summary><span className="command-chip">{command}</span>{args && <span className="tool-summary">{args}</span>}<Timestamp timestamp={relationships.commandOutputs.get(index)?.timestamp ?? entry.timestamp} now={now} /></summary>{(args || output) && <div className="entry-detail">{args && <><h4>Arguments</h4><pre>{args}</pre></>}{output && <><h4>Output</h4><pre>{output}</pre></>}</div>}</details>
  }
  if (entry.kind === 'compact_divider') {
    const metadata = objectValue(payload.compactMetadata)
    if (Object.keys(metadata).length > 0) return <div className="compaction-divider"><span>context compacted ({valueText(metadata.trigger) || 'unknown'}, {Number(metadata.preTokens).toLocaleString()} → {Number(metadata.postTokens).toLocaleString()} tokens)</span><Timestamp timestamp={entry.timestamp} now={now} /></div>
    return <details className="entry-expander compact-summary"><summary>compaction summary<Timestamp timestamp={entry.timestamp} now={now} /></summary><div className="entry-detail markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{content || valueText(payload.content)}</ReactMarkdown></div></details>
  }
  if (entry.kind === 'turn_duration') return <div className="turn-footer">turn · {formatDuration(Number(payload.durationMs))}{payload.messageCount != null ? ` · ${Number(payload.messageCount)} messages` : ''} · <Timestamp timestamp={entry.timestamp} now={now} /></div>
  if (entry.kind === 'task_notification' || entry.kind === 'injected_system') return <details className="system-chip"><summary>{entry.kind === 'task_notification' ? 'background task finished' : 'injected system prompt'} · <Timestamp timestamp={entry.timestamp} now={now} /></summary><div>{content || JSON.stringify(payload)}</div></details>
  if (entry.kind === 'system_chip') return !showSystem && payload.subtype !== 'scheduled_task_fire' ? null : <details className="system-chip"><summary>{valueText(payload.subtype) || 'system entry'} · <Timestamp timestamp={entry.timestamp} now={now} /></summary><pre>{JSON.stringify(payload, null, 2)}</pre></details>
  if (!showSystem) return null
  return <details className="system-chip unknown-entry"><summary>{entry.quarantine ? `quarantined entry · ${entry.quarantine.reason}` : `unknown entry · ${entry.kind}`} · <Timestamp timestamp={entry.timestamp} now={now} /></summary><pre>{JSON.stringify(entry.payload, null, 2)}</pre></details>
}

export function TranscriptEntries({ entries, agentName, now, showSystem }: { entries: TranscriptEntry[], agentName: string, now: number, showSystem: boolean }) {
  const relationships = useMemo(() => relateEntries(entries), [entries])
  return <>{entries.map((entry, index) => <EntryView entry={entry} index={index} entries={entries} relationships={relationships} agentName={agentName} now={now} showSystem={showSystem} key={entry.uuid || `${entry.byteOffset}:${entry.line}`} />)}</>
}
