import type { Board } from '../../types.ts'
import { findAgentRow } from '../../shared/agentStatus.ts'
import { terminalTheme } from '../screen/terminalTheme.ts'

// The herdr view: herdr itself, one client per browser, in a PTY the serve
// owns (GET /api/herdr/term upgraded to a WebSocket; docs/web-api-contract.md,
// herdr view amendment). Binary frames are bytes both ways; text frames are
// JSON control (resize, focus) up and notices (focused, focus_failed, error,
// exit) down.

export const herdrTermPath = '/api/herdr/term'
// Leaving the view keeps the client this long, so flipping back is instant.
export const leaveGraceMs = 60_000
export const resizeDebounceMs = 120

// The screen panels hide xterm's cursor (it is the background colour) since
// they only show a pane; in the herdr view the owner types, so the caret is
// a bright block with the character under it in the background colour.
export const herdrTerminalTheme = {
  ...terminalTheme,
  cursor: terminalTheme.brightWhite,
  cursorAccent: terminalTheme.background,
}

const firstRetryMs = 500
const maxRetryMs = 10_000

export function herdrTermURL(location: Pick<Location, 'protocol' | 'host'>, size?: { cols: number, rows: number }) {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const query = size ? `?cols=${size.cols}&rows=${size.rows}` : ''
  return `${scheme}//${location.host}${herdrTermPath}${query}`
}

export function resizeFrame(cols: number, rows: number) {
  return JSON.stringify({ type: 'resize', cols, rows })
}

export function focusFrame(paneID: string) {
  return JSON.stringify({ type: 'focus', pane_id: paneID })
}

export type HerdrNotice =
  | { type: 'focused', pane_id: string }
  | { type: 'focus_failed', pane_id: string, detail: string }
  | { type: 'error', detail: string }
  | { type: 'exit', code?: number }

export function parseNotice(text: string): HerdrNotice | null {
  try {
    const value = JSON.parse(text) as { type?: unknown }
    if (value && typeof value === 'object' && typeof value.type === 'string' && ['focused', 'focus_failed', 'error', 'exit'].includes(value.type)) return value as HerdrNotice
  } catch {
    // Not JSON: not a notice.
  }
  return null
}

// reconnectDelay doubles from half a second to ten seconds.
export function reconnectDelay(attempt: number) {
  return Math.min(maxRetryMs, firstRetryMs * 2 ** Math.max(0, attempt))
}

// The connection's life as the header says it: connecting the first time,
// live, reconnecting after a drop (a serve restart), or ended because the
// herdr client itself exited (herdr's own detach, say) — that one waits for
// the owner rather than reattaching under them.
export type HerdrConnection = 'idle' | 'connecting' | 'live' | 'reconnecting' | 'ended'

export function connectionLabel(connection: HerdrConnection, retryInMs?: number) {
  switch (connection) {
    case 'idle': return 'not connected'
    case 'connecting': return 'connecting…'
    case 'live': return 'live'
    case 'reconnecting': return retryInMs ? `reconnecting in ${Math.ceil(retryInMs / 1000)} s…` : 'reconnecting…'
    case 'ended': return 'herdr exited'
  }
}

// followTarget is the pane of the agent the web is on: the lens's selected
// card's visible agent, an agent opened alone, the zoom's active agent, or
// the active terminal panel's own pane.
// The board writes "-" for an agent with no visible pane.
const realPane = (pane: string | undefined) => pane && pane !== '-' ? pane : null

export function followTarget(board: Board | undefined, view: { onLens: boolean, lensAgent?: string, aloneAgent?: string, activeAgent?: string, activePane?: string }): string | null {
  if (!view.onLens && !view.aloneAgent && view.activePane) return realPane(view.activePane)
  const agent = view.onLens ? view.lensAgent : view.aloneAgent ?? view.activeAgent
  if (!agent) return null
  return realPane(findAgentRow(board, agent)?.pane_id)
}

// shouldFollow sends herdr the switch once per new target while the view is
// open, live and following; a reconnect (sent reset to null) sends it again.
export function shouldFollow(state: { open: boolean, follow: boolean, connection: HerdrConnection, sent: string | null }, target: string | null): target is string {
  return state.open && state.follow && state.connection === 'live' && target !== null && target !== state.sent
}
