import type { Board, Row } from '../../types.ts'
import { findAgentRow } from '../../shared/agentStatus.ts'
import { agentBlocked } from '../spaces/spaceAttentionModel.ts'
import type { SpaceMember } from '../spaces/index.ts'
import type { CardTextMode } from './cardTextModel.ts'

// The lens is the home view: every space as a card in one of three rows
// the owner picks (1 2 3). Lens order is the focus row, then watch, then
// background, each row in space order. Native reference:
// tools/herder/native/src/views/lens.rs and store/spaces.rs.

export type LensRow = 'focus' | 'watch' | 'background'
export const lensRows: readonly LensRow[] = ['focus', 'watch', 'background']
// A space never placed sits in watch.
export const defaultLensRow: LensRow = 'watch'

// LensPrefs are this browser's lens choices: each space's row and each
// space's visible agent (v), and the card text (t) and focus size (s),
// answer and the first size while unset.
export type LensPrefs = Readonly<{ rows: Readonly<Record<string, LensRow>>, visible: Readonly<Record<string, string>>, text?: CardTextMode, size?: number, peek?: number }>
export const emptyLensPrefs: LensPrefs = { rows: {}, visible: {} }

// The view: the lens, a zoom into the active space's dock, or one agent in
// no space opened alone.
export type LensView = { kind: 'lens' } | { kind: 'zoom' } | { kind: 'alone', agent: string }

type SpaceLike = { id: string }

export function rowOf(prefs: LensPrefs, spaceID: string): LensRow {
  return prefs.rows[spaceID] ?? defaultLensRow
}

// lensRowsOf groups the spaces (already in space order) into the three rows.
export function lensRowsOf<S extends SpaceLike>(spaces: readonly S[], prefs: LensPrefs): Record<LensRow, S[]> {
  const rows: Record<LensRow, S[]> = { focus: [], watch: [], background: [] }
  for (const space of spaces) rows[rowOf(prefs, space.id)].push(space)
  return rows
}

export function lensOrder(spaces: readonly SpaceLike[], prefs: LensPrefs): string[] {
  const rows = lensRowsOf(spaces, prefs)
  return lensRows.flatMap((row) => rows[row].map((space) => space.id))
}

// selectedSpace is the selection while its space is still on the lens,
// else the implicit first card.
export function selectedSpace(order: readonly string[], selected: string | null): string | null {
  return selected && order.includes(selected) ? selected : order[0] ?? null
}

// stepCard is ← → h l: the previous or next card in lens order, crossing
// rows, stopping at the ends.
export function stepCard(order: readonly string[], selected: string, by: -1 | 1): string {
  const at = order.indexOf(selected)
  return order[Math.max(0, Math.min(order.length - 1, (at < 0 ? 0 : at) + by))] ?? selected
}

// stepRow is ↓ ↑ (j k): the same column in the next or previous row that has
// cards, else that row's last card; at the edge the selection stays.
export function stepRow(spaces: readonly SpaceLike[], prefs: LensPrefs, selected: string, by: -1 | 1): string {
  const rows = lensRowsOf(spaces, prefs)
  const here = lensRows.indexOf(rowOf(prefs, selected))
  const column = Math.max(0, rows[lensRows[here]!].findIndex((space) => space.id === selected))
  for (let index = here + by; index >= 0 && index < lensRows.length; index += by) {
    const row = rows[lensRows[index]!]
    const target = row[column] ?? row.at(-1)
    if (target) return target.id
  }
  return selected
}

// spaceStep is ⇧⌥←/→ in a zoom: the previous or next space in lens order,
// wrapping.
export function spaceStep(order: readonly string[], from: string | null, by: -1 | 1): string | null {
  if (order.length === 0) return null
  const at = from ? order.indexOf(from) : -1
  if (at < 0) return order[by > 0 ? 0 : order.length - 1]!
  return order[(at + by + order.length) % order.length]!
}

// liveMembers are a space's agent members on the board, in dock order.
export function liveMembers(members: readonly SpaceMember[], board: Board | undefined): string[] {
  return members.flatMap((member) => member.kind === 'agent' && findAgentRow(board, member.name) ? [member.name] : [])
}

// visibleAgent is the card's agent: the owner's pick while it is still a
// member on the board, else the first member on the board.
export function visibleAgent(live: readonly string[], pick: string | undefined): string | undefined {
  return pick && live.includes(pick) ? pick : live[0]
}

// cycleVisible is v: the next member on the board after the visible one,
// wrapping; undefined with no member on the board.
export function cycleVisible(live: readonly string[], pick: string | undefined): string | undefined {
  const current = visibleAgent(live, pick)
  if (current === undefined) return undefined
  return live[(live.indexOf(current) + 1) % live.length]
}

// entryAgent is where a zoom lands in a space with no last panel to return
// to: the space's first member on the board that needs the owner, else its
// visible agent.
export function entryAgent(live: readonly string[], pick: string | undefined, needsYou: (name: string) => boolean): string | undefined {
  return live.find(needsYou) ?? visibleAgent(live, pick)
}

// zoomLanding is the agent a zoom into a space lands on (owner #463121,
// over the spec's entry point): the panel the owner last had focused there,
// kept as the layout holds it, while it is a file or an agent still on the
// board; only without one, the entry agent. undefined keeps the layout's
// focused panel as it is.
export function zoomLanding(last: { kind: string, name?: string } | undefined, onBoard: (name: string) => boolean, entry: () => string | undefined): string | undefined {
  if (!last) return entry()
  if (last.kind !== 'agent') return undefined
  return last.name !== undefined && onBoard(last.name) ? last.name : entry()
}

// looseAgents are the board agents in no space, by name: n and N reach them
// after the spaces, each opened alone.
export function looseAgents(boardAgents: Iterable<string>, membersBySpace: Readonly<Record<string, readonly SpaceMember[]>>): string[] {
  const held = new Set(Object.values(membersBySpace).flatMap((members) => members.flatMap((member) => member.kind === 'agent' ? [member.name] : [])))
  return [...boardAgents].filter((name) => !held.has(name)).sort()
}

export type LensStop = { kind: 'space', id: string } | { kind: 'alone', agent: string }

function sameStop(left: LensStop, right: LensStop | null) {
  return right !== null && (left.kind === 'space' ? right.kind === 'space' && right.id === left.id : right.kind === 'alone' && right.agent === left.agent)
}

// nextNeeding is n / N: the first stop after from that needs the owner,
// wrapping round to from itself last. The stops are the spaces in lens
// order, then (with alone: N, or n inside a zoom) the loose agents. With no
// from, or one no longer a stop, the search starts at the top.
export function nextNeeding({ order, loose, from, alone, spaceNeeds, agentNeeds }: {
  order: readonly string[]
  loose: readonly string[]
  from: LensStop | null
  alone: boolean
  spaceNeeds: (id: string) => boolean
  agentNeeds: (name: string) => boolean
}): LensStop | null {
  const stops: LensStop[] = [
    ...order.map((id): LensStop => ({ kind: 'space', id })),
    ...(alone ? loose.map((agent): LensStop => ({ kind: 'alone', agent })) : []),
  ]
  const at = stops.findIndex((stop) => sameStop(stop, from))
  const start = at < 0 ? 0 : at + 1
  for (let step = 0; step < stops.length; step += 1) {
    const stop = stops[(start + step) % stops.length]!
    if (stop.kind === 'space' ? spaceNeeds(stop.id) : agentNeeds(stop.agent)) return stop
  }
  return null
}

// LensStatus is native's: blocked (bus or herdr) over working (herdr) over
// done over idle.
export type LensStatus = 'blocked' | 'working' | 'done' | 'idle'

export function lensStatus(row: Row | undefined): LensStatus {
  if (agentBlocked(row)) return 'blocked'
  if (row?.herdr_status === 'working') return 'working'
  if (row?.herdr_status === 'done') return 'done'
  return 'idle'
}

export const statusGlyph: Record<Exclude<LensStatus, 'working'>, string> = { blocked: '■', done: '✓', idle: '○' }

// statusText is a card's fallback body: "<label> · <title>", the label
// "your turn" while the agent needs the owner, else its status. The answer
// text replaces it once loaded (cardTextModel).
export function statusText(row: Row | undefined, needs: boolean): string {
  if (!row) return 'no agent on the board'
  const label = needs ? 'your turn' : lensStatus(row)
  return row.title ? `${label} · ${row.title}` : label
}

// Each row's card: its width and body lines. Focus takes the card size
// (284px, five lines by default; s cycles it), watch keeps the focus width
// at two lines, background is a compact card with no body.
export const focusSizes: readonly (readonly [number, number])[] = [[284, 5], [360, 8], [440, 12], [240, 3]]

export function cardShape(row: LensRow, size = 0): { width: number, lines: number } {
  const [width, lines] = focusSizes[size % focusSizes.length]!
  if (row === 'focus') return { width, lines }
  if (row === 'watch') return { width, lines: 2 }
  return { width: focusSizes[3]![0], lines: 0 }
}

export const rowHeads: Record<LensRow, string> = { focus: 'FOCUS last answer', watch: 'WATCH two lines', background: 'BACKGROUND no text' }

// CardLook is the card's brightness: needs-you is bright with the accent
// wash, quiet working is three quarters, other quiet is half; the selection
// is always full with the amber border.
export type CardLook = { needs: boolean, selected: boolean, opacity: 1 | 0.75 | 0.5 }

export function cardLook(needs: number, selected: boolean, status: LensStatus | null): CardLook {
  const opacity = needs > 0 || selected ? 1 : status === 'working' ? 0.75 : 0.5
  return { needs: needs > 0, selected, opacity }
}

// whoLine is the card's who line: the visible agent (— with none) and +N
// for the other members, files included.
export function whoLine(members: readonly SpaceMember[], visible: string | undefined): { name: string, others: number } {
  return { name: visible ?? '—', others: members.length - (visible === undefined ? 0 : 1) }
}

export function headerLine({ live, spaces, needYou, serverUpdated }: { live: boolean, spaces: number, needYou: number, serverUpdated: boolean }): string {
  return `herder · ${live ? 'live' : 'offline'} · ${spaces} space${spaces === 1 ? '' : 's'} · ${needYou} need you${serverUpdated ? ' · server updated' : ''} · ? keys`
}

// crumbParts is the zoom's crumb: lens › <space> › <agent>. A zoom of no
// space reads "no space"; a space deleted under the zoom "(space gone)".
export function crumbParts(view: LensView, space: { name: string } | undefined, agent: string | undefined): { space: string, agent: string | undefined, alone: boolean } {
  if (view.kind === 'alone') return { space: 'no space', agent: view.agent, alone: true }
  return { space: space?.name ?? '(space gone)', agent, alone: false }
}

// revealScroll is the least scroll that shows a card whole: none when it
// already fits, else just enough to bring its nearer edge in.
export function revealScroll(view: { top: number, bottom: number }, card: { top: number, bottom: number }, scrollTop: number): number {
  if (card.top < view.top) return scrollTop - (view.top - card.top)
  if (card.bottom > view.bottom) return scrollTop + Math.min(card.bottom - view.bottom, card.top - view.top)
  return scrollTop
}
