import type { Board, TranscriptEntry } from '../../types.ts'
import { parseAssistantFencing } from '../transcript/fencingModel.ts'

// The lens cards' text (spec §2, native store/cards.rs): each focus and
// watch card's visible agent's last assistant answer, as one plain
// paragraph. Background cards carry none, so their agents are never read.
//
// One tail read (cardTail entries) per agent per turn: a read is keyed on
// the board's turn_end_id, so an agent is read again only once a turn ends.
// At most cardReadCap reads run at once, one per agent. A result for a turn
// other than the agent's current one, or for an agent no longer on a text
// card, is dropped, success or failure; the current turn is then read. A
// failed read keeps the text it had and is not asked again for that turn.

export const cardTail = 12
export const cardReadCap = 4

// A turn key: the board's turn_end_id, null before the serve has seen one.
export type CardTurn = number | null

export type CardReads = Readonly<{
  // Per agent, the turn last read and the last answer found (kept when a read finds none).
  read: Readonly<Record<string, Readonly<{ turn: CardTurn, text: string | null }>>>
  // Per agent, the turn being read.
  flight: Readonly<Record<string, CardTurn>>
  // Per agent, the turn whose read failed.
  failed: Readonly<Record<string, CardTurn>>
}>

export const noCardReads: CardReads = { read: {}, flight: {}, failed: {} }

export type CardRead = { agent: string, turn: CardTurn }

// dueCardReads is what to read now: each carded agent whose turn has moved
// past its last read, not in flight and not failed for that turn, while
// slots are free, in carded order.
export function dueCardReads(state: CardReads, carded: ReadonlyMap<string, CardTurn>, cap = cardReadCap): CardRead[] {
  const room = cap - Object.keys(state.flight).length
  if (room <= 0) return []
  const due: CardRead[] = []
  for (const [agent, turn] of carded) {
    if (due.length >= room) break
    const read = state.read[agent]
    if (read && read.turn === turn) continue
    if (agent in state.flight) continue
    if (agent in state.failed && state.failed[agent] === turn) continue
    due.push({ agent, turn })
  }
  return due
}

export function startCardReads(state: CardReads, reads: readonly CardRead[]): CardReads {
  if (reads.length === 0) return state
  const flight = { ...state.flight }
  for (const { agent, turn } of reads) flight[agent] = turn
  return { ...state, flight }
}

// landCardRead takes a read's result: entries, or null for a failed read.
// carded is what the cards show now (agent → current turn).
export function landCardRead(state: CardReads, read: CardRead, entries: readonly TranscriptEntry[] | null, carded: ReadonlyMap<string, CardTurn>): CardReads {
  const flight = { ...state.flight }
  delete flight[read.agent]
  const settled = { ...state, flight }
  if (!carded.has(read.agent) || carded.get(read.agent) !== read.turn) return settled
  if (entries === null) return { ...settled, failed: { ...state.failed, [read.agent]: read.turn } }
  const text = lastAnswer(entries) ?? state.read[read.agent]?.text ?? null
  return { ...settled, read: { ...state.read, [read.agent]: { turn: read.turn, text } } }
}

// keepCardReads lets go of the answers of agents no longer on the board.
export function keepCardReads(state: CardReads, onBoard: (agent: string) => boolean): CardReads {
  const read = Object.fromEntries(Object.entries(state.read).filter(([agent]) => onBoard(agent)))
  return Object.keys(read).length === Object.keys(state.read).length ? state : { ...state, read }
}

export function cardAnswer(state: CardReads, agent: string): string | null {
  return state.read[agent]?.text ?? null
}

// lastAnswer is the newest assistant answer in entries with text left once cleaned.
export function lastAnswer(entries: readonly TranscriptEntry[]): string | null {
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    const text = answerText(entries[index]!)
    if (text) return text
  }
  return null
}

const record = (value: unknown): Record<string, unknown> => value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}

// answerText is an assistant answer's text, cleaned as the transcript
// cleans it with <status> dropped too (the status line is not the card
// text), as one plain paragraph; null when nothing is left. Thinking
// blocks and API errors are not answers.
export function answerText(entry: TranscriptEntry): string | null {
  if (entry.kind !== 'assistant_text') return null
  const payload = record(entry.payload)
  if (payload.isApiErrorMessage === true) return null
  const content = record(payload.message).content ?? payload.content
  const text = typeof content === 'string' ? content
    : Array.isArray(content) ? content.flatMap((block) => { const part = record(block); return part.type === 'text' && typeof part.text === 'string' ? [part.text] : [] }).join('\n')
      : ''
  const visible = parseAssistantFencing(text).segments.flatMap((segment) => segment.kind === 'text' ? [segment.content] : []).join('\n')
  const flat = flattenAnswer(visible)
  return flat || null
}

const tableRule = /^[|:\-\s]+$/

// flattenAnswer is markdown as one paragraph for the card's line clamp:
// fenced code marks, heading and quote marks, list bullets, table rules,
// emphasis, code ticks and link targets dropped (a link keeps its text, an
// image its alt), each table row its cells joined by · and closed by ;,
// and lines and runs of space joined.
export function flattenAnswer(markdown: string): string {
  const lines = markdown.split(/\r?\n/).flatMap((raw) => {
    const line = raw.trim()
    if (!line || /^(?:```|~~~)/.test(line) || /^(?:[-*_]\s*){3,}$/.test(line) || (line.includes('-') && tableRule.test(line))) return []
    if (line.startsWith('|')) return [`${line.split('|').map((cell) => cell.trim()).filter(Boolean).join(' · ')};`]
    return [line.replace(/^(?:>\s*)+/, '').replace(/^#{1,6}\s+/, '').replace(/^(?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?/, '')]
  })
  return lines.join(' ')
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/(\*\*|__)(?=\S)(.+?)(?<=\S)\1/g, '$2')
    .replace(/(^|[^\w*])\*(?=\S)([^*]+?)(?<=\S)\*(?![\w*])/g, '$1$2')
    .replace(/(^|[^\w])_(?=\S)([^_]+?)(?<=\S)_(?!\w)/g, '$1$2')
    .replace(/~~(?=\S)(.+?)(?<=\S)~~/g, '$1')
    .replace(/`+/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

// t: the card text answer → status → cwd.
export type CardTextMode = 'answer' | 'status' | 'cwd'
export const cardTextModes: readonly CardTextMode[] = ['answer', 'status', 'cwd']

export function nextCardText(mode: CardTextMode): CardTextMode {
  return cardTextModes[(cardTextModes.indexOf(mode) + 1) % cardTextModes.length]!
}

// s: the focus card size, an index into lensModel's focusSizes.
export function nextCardSize(size: number, sizes: number): number {
  return (size + 1) % sizes
}

// agentFolder is the cwd card text: the agent's workspace folder, else the
// workspace's name; an agent in no workspace has none.
export function agentFolder(board: Board | undefined, name: string): string {
  const has = (rows: readonly { agent: string, subagents?: readonly { agent: string }[] }[]): boolean =>
    rows.some((row) => row.agent === name || has(row.subagents ?? []))
  const workspace = board?.workspaces.find((candidate) => candidate.tabs.some((tab) => has(tab.panes)))
  return workspace ? workspace.cwd || workspace.label : 'no folder'
}
