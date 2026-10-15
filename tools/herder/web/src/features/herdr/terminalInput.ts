// What the herdr view's ghostty surface sends for the mouse and for Option:
// ghostty-web 0.4.0 parses mouse-tracking modes but never reports the mouse,
// and its key encoder has no Option-as-Meta, so the surface encodes both.

// The DEC private modes a client sets to track the mouse, and how it wants
// the reports written.
export const mouseModeNumbers = { x10: 9, normal: 1000, button: 1002, any: 1003, utf8: 1005, sgr: 1006, urxvt: 1015 } as const

export type MouseTracking = 'off' | 'x10' | 'normal' | 'button' | 'any'
export type MouseEncoding = 'default' | 'utf8' | 'sgr' | 'urxvt'
export type MouseModes = { tracking: MouseTracking, encoding: MouseEncoding }

// mouseModes reads the client's current choice; the widest tracking wins.
export function mouseModes(getMode: (mode: number) => boolean): MouseModes {
  const tracking: MouseTracking = getMode(mouseModeNumbers.any) ? 'any'
    : getMode(mouseModeNumbers.button) ? 'button'
    : getMode(mouseModeNumbers.normal) ? 'normal'
    : getMode(mouseModeNumbers.x10) ? 'x10'
    : 'off'
  const encoding: MouseEncoding = getMode(mouseModeNumbers.sgr) ? 'sgr'
    : getMode(mouseModeNumbers.urxvt) ? 'urxvt'
    : getMode(mouseModeNumbers.utf8) ? 'utf8'
    : 'default'
  return { tracking, encoding }
}

// A pointer event in cell terms: col and row count from 1. button is the
// DOM's (0 left, 1 middle, 2 right) for press, release and a held-button
// move; null for a move with nothing held. A wheel notch is up or down.
export type MouseAction =
  | { kind: 'press' | 'release', button: number }
  | { kind: 'move', button: number | null }
  | { kind: 'wheel', direction: 'up' | 'down' }

export type MouseAt = { col: number, row: number, shift: boolean, alt: boolean, ctrl: boolean }

// wantsMove is true when the client asked to hear this move: any-event
// tracking hears every move, button-event tracking only drags.
export function wantsMove(modes: MouseModes, held: boolean) {
  return modes.tracking === 'any' || (modes.tracking === 'button' && held)
}

// mouseReport is the bytes the client asked for, or null when the mode
// reports nothing for this action. The default encoding is raw bytes (a
// coordinate past 223 cannot be said in it); the others are text.
export function mouseReport(modes: MouseModes, action: MouseAction, at: MouseAt): string | Uint8Array | null {
  if (modes.tracking === 'off') return null
  if (modes.tracking === 'x10' && action.kind !== 'press') return null
  if (action.kind === 'move' && !wantsMove(modes, action.button !== null)) return null
  if (action.kind !== 'wheel' && action.button !== null && (action.button < 0 || action.button > 2)) return null
  let code = action.kind === 'wheel' ? (action.direction === 'up' ? 64 : 65)
    : action.kind === 'move' ? 32 + (action.button ?? 3)
    : action.button
  // Only SGR says which button let go; the older encodings say "released".
  const released = action.kind === 'release'
  if (released && modes.encoding !== 'sgr') code = 3
  // X10 compatibility mode reports no modifiers.
  if (modes.tracking !== 'x10') code += (at.shift ? 4 : 0) + (at.alt ? 8 : 0) + (at.ctrl ? 16 : 0)
  switch (modes.encoding) {
    case 'sgr': return `\x1b[<${code};${at.col};${at.row}${released ? 'm' : 'M'}`
    case 'urxvt': return `\x1b[${code + 32};${at.col};${at.row}M`
    case 'utf8': {
      if (at.col > 2015 || at.row > 2015) return null
      return `\x1b[M${String.fromCodePoint(code + 32, at.col + 32, at.row + 32)}`
    }
    case 'default': {
      if (at.col > 223 || at.row > 223) return null
      return Uint8Array.of(0x1b, 0x5b, 0x4d, code + 32, at.col + 32, at.row + 32)
    }
  }
}

// cellAt maps a point in the grid's box to its 1-based cell, clamped to the grid.
export function cellAt(point: { x: number, y: number }, box: { left: number, top: number, width: number, height: number }, grid: { cols: number, rows: number }) {
  const clamp = (value: number, size: number) => Math.min(size, Math.max(1, value))
  return {
    col: clamp(Math.floor((point.x - box.left) / (box.width / grid.cols)) + 1, grid.cols),
    row: clamp(Math.floor((point.y - box.top) / (box.height / grid.rows)) + 1, grid.rows),
  }
}

// The printable keys by physical position, unshifted and shifted (US
// layout), so ⌥ sends the key's own character whatever a Mac makes of it.
const punctuation: Record<string, [string, string]> = {
  Backquote: ['`', '~'], Minus: ['-', '_'], Equal: ['=', '+'], BracketLeft: ['[', '{'], BracketRight: [']', '}'],
  Backslash: ['\\', '|'], Semicolon: [';', ':'], Quote: ['\'', '"'], Comma: [',', '<'], Period: ['.', '>'], Slash: ['/', '?'],
}
const shiftedDigits = ')!@#$%^&*('

// metaKey is what ⌥ plus a printable key sends with Option as Meta: ESC
// and the key's character (⌥W is herdr's close-pane). Anything else —
// another modifier held, or a key with no character — is null and left to
// the terminal's own encoder.
export function metaKey(event: Pick<KeyboardEvent, 'code' | 'altKey' | 'ctrlKey' | 'metaKey' | 'shiftKey'>): string | null {
  if (!event.altKey || event.ctrlKey || event.metaKey) return null
  const letter = /^Key([A-Z])$/.exec(event.code)?.[1]
  if (letter) return `\x1b${event.shiftKey ? letter : letter.toLowerCase()}`
  const digit = /^Digit([0-9])$/.exec(event.code)?.[1]
  if (digit) return `\x1b${event.shiftKey ? shiftedDigits[Number(digit)] : digit}`
  const pair = punctuation[event.code]
  if (pair) return `\x1b${pair[event.shiftKey ? 1 : 0]}`
  if (event.code === 'Space') return '\x1b '
  return null
}
