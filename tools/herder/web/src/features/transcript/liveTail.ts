import { createElement, type CSSProperties } from 'react'
import type { ScreenFrame } from '../../types.ts'

// The live tail shows this many trailing non-empty screen rows under the
// transcript while the agent is working. Also the CSS block height.
export const liveTailRowCount = 8

// ESC sequences the herdr ANSI projection can carry: CSI (colours, cursor),
// OSC (titles, hyperlinks) ended by BEL or ST, and single-character escapes.
// eslint-disable-next-line no-control-regex
const ansiPattern = /\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[0-9=>@-Z\\-_]/g

export function stripAnsi(text: string) {
  return text.replace(ansiPattern, '')
}

// The last n non-empty rows of a screen frame as plain text. Anything but an
// available frame projects to no rows: the region then says why instead.
export function screenTailRows(frame: ScreenFrame | undefined, n = liveTailRowCount): string[] {
  if (!frame || frame.status !== 'available' || n <= 0) return []
  const rows = stripAnsi(frame.text).split('\n').map((row) => row.replace(/\r/g, '').trimEnd()).filter((row) => row.length > 0)
  return rows.slice(-n)
}

// The tail exists only while the operator can see it forming: the panel is
// visible in the dock, the agent is working now, and the transcript (not the
// screen) is the view. Each condition leaving unmounts the region, which also
// drops the pane's screen subscription.
export function liveTailShown({ visible, status, screenMode }: { visible: boolean, status: string, screenMode: boolean }) {
  return visible && status === 'active' && !screenMode
}

export function liveTailNotice(frame: ScreenFrame | undefined) {
  if (!frame) return 'waiting for the pane screen…'
  if (frame.status !== 'available') return frame.detail || 'pane screen unavailable'
  return ''
}

// Presentation only: the rows already projected from a frame. Rendered by
// TranscriptLiveTail and directly by node tests (a .ts module, no JSX).
export function LiveTailRegion({ rows, status, notice }: { rows: string[], status: string, notice: string }) {
  return createElement('aside', { className: 'transcript-live-tail', 'aria-label': 'Live screen tail', style: { '--live-tail-rows': liveTailRowCount } as CSSProperties },
    createElement('div', { className: 'live-tail-label' }, createElement('span', { className: 'live-tail-word' }, 'live'), createElement('span', { className: 'live-tail-status' }, status)),
    rows.length > 0 ? createElement('pre', { className: 'live-tail-rows' }, rows.join('\n')) : createElement('div', { className: 'live-tail-notice' }, notice))
}
