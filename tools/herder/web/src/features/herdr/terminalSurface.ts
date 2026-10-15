import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { terminalTheme } from '../screen/terminalTheme.ts'

// TerminalSurface is all the herdr view asks of a terminal emulator, so
// ghostty-web (libghostty WASM) can replace xterm.js behind it later.
export interface TerminalSurface {
  write(bytes: Uint8Array): void
  // onInput delivers what the owner typed or pasted, as the bytes to send.
  onInput(listener: (data: string | Uint8Array) => void): () => void
  // fit sizes the grid to the host and answers the new grid.
  fit(): { cols: number, rows: number } | null
  focus(): void
  // reset clears the screen for a new client; herdr repaints on attach.
  reset(): void
  dispose(): void
}

export type TerminalSurfaceOptions = {
  // ownKey is true for a key the page keeps (the leave chord): the
  // terminal neither sends nor swallows it.
  ownKey: (event: KeyboardEvent) => boolean
}

export type TerminalSurfaceFactory = (host: HTMLElement, options: TerminalSurfaceOptions) => TerminalSurface

export const createXtermSurface: TerminalSurfaceFactory = (host, { ownKey }) => {
  const terminal = new Terminal({
    allowProposedApi: false,
    cursorBlink: true,
    fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
    fontSize: 13,
    lineHeight: 1.1,
    // herdr owns scrollback; the browser keeps none of its own.
    scrollback: 0,
    macOptionIsMeta: true,
    theme: terminalTheme,
  })
  const fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.attachCustomKeyEventHandler((event) => !ownKey(event))
  terminal.open(host)
  const latin1 = (data: string) => Uint8Array.from(data, (char) => char.charCodeAt(0) & 0xff)
  return {
    write: (bytes) => terminal.write(bytes),
    onInput: (listener) => {
      const text = terminal.onData((data) => listener(data))
      // Mouse reports in some modes arrive as raw bytes in a binary string.
      const binary = terminal.onBinary((data) => listener(latin1(data)))
      return () => { text.dispose(); binary.dispose() }
    },
    fit: () => {
      const proposal = fitAddon.proposeDimensions()
      if (!proposal || !Number.isFinite(proposal.cols) || !Number.isFinite(proposal.rows)) return null
      const cols = Math.max(2, proposal.cols)
      const rows = Math.max(2, proposal.rows)
      if (terminal.cols !== cols || terminal.rows !== rows) terminal.resize(cols, rows)
      return { cols, rows }
    },
    focus: () => terminal.focus(),
    reset: () => terminal.reset(),
    dispose: () => terminal.dispose(),
  }
}
