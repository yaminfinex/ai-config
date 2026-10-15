import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { herdrTerminalTheme } from './herdrViewModel.ts'

// TerminalSurface is all the herdr view asks of a terminal emulator:
// xterm.js by default (see preferredSurface), or ghostty-web (libghostty as
// WASM, loaded when the view first opens) with xterm.js when that cannot load.
// A right-click on the host (⌃-click on a Mac) never opens the browser's
// context menu: the button is herdr's, reported to it like any other when
// it tracks the mouse, and selects nothing in the browser.
export interface TerminalSurface {
  readonly kind: TerminalKind
  write(bytes: Uint8Array): void
  // onInput delivers what the owner typed or pasted, as the bytes to send.
  onInput(listener: (data: string | Uint8Array) => void): () => void
  // fit sizes the grid to the host and answers the new grid.
  fit(): { cols: number, rows: number } | null
  focus(): void
  // reset clears the screen for a new client; herdr repaints on attach.
  reset(): void
  // screen reads the visible rows back as text, and where the caret is.
  screen(): TerminalScreen
  dispose(): void
}

export type TerminalKind = 'ghostty' | 'xterm'
export type TerminalScreen = { cols: number, rows: string[], cursor: { col: number, row: number } }

export type TerminalSurfaceOptions = {
  // ownKey is true for a key the page keeps (the leave chord): the
  // terminal neither sends nor swallows it.
  ownKey: (event: KeyboardEvent) => boolean
}

export type TerminalSurfaceFactory = (host: HTMLElement, options: TerminalSurfaceOptions) => TerminalSurface | Promise<TerminalSurface>

// The surface on each host, for whoever needs to read the screen back.
const surfaces = new WeakMap<Element, TerminalSurface>()
export const surfaceOf = (host: Element) => surfaces.get(host)

// The ghostty surface: ghostty-web's module (and its WASM, inlined in it)
// is fetched from this serve only when the herdr view first opens.
export const createGhosttySurface: TerminalSurfaceFactory = async (host, options) => (await import('./ghosttySurface.ts')).openGhosttySurface(host, options)

// The herdr view runs on xterm.js unless this browser opts into ghostty-web
// (localStorage terminalPreferenceKey = 'ghostty'): ghostty-web 0.4.0
// repaints every frame for as long as the view is mounted, hidden or not,
// which held the owner's tab near 80% CPU.
export const terminalPreferenceKey = 'herder.web.herdrTerminal.v1'
export const preferredSurface: TerminalSurfaceFactory = (host, options) => {
  let ghostty = false
  try {
    ghostty = localStorage.getItem(terminalPreferenceKey) === 'ghostty'
  } catch {
    // No storage: the default stands.
  }
  return (ghostty ? createGhosttySurface : createXtermSurface)(host, options)
}

// openTerminalSurface makes the preferred surface on host or, when that
// fails (the WASM did not load or start), xterm.js, saying why. Once signal
// aborts (the view unmounted while it loaded) the surface is disposed and
// the answer is null.
export async function openTerminalSurface(host: HTMLElement, options: TerminalSurfaceOptions, preferred: TerminalSurfaceFactory, signal: AbortSignal): Promise<{ surface: TerminalSurface, fallback?: string } | null> {
  let surface: TerminalSurface
  let fallback: string | undefined
  try {
    surface = await preferred(host, options)
  } catch (error) {
    if (signal.aborted) return null
    surface = createXtermSurface(host, options)
    fallback = error instanceof Error ? error.message : String(error)
  }
  if (signal.aborted) {
    surface.dispose()
    return null
  }
  surfaces.set(host, surface)
  return { surface: { ...surface, dispose: () => { surfaces.delete(host); surface.dispose() } }, fallback }
}

export const createXtermSurface = (host: HTMLElement, { ownKey }: TerminalSurfaceOptions): TerminalSurface => {
  const terminal = new Terminal({
    allowProposedApi: false,
    cursorBlink: true,
    // A solid block while the view has the keys; an outline only once focus leaves.
    cursorStyle: 'block',
    cursorInactiveStyle: 'outline',
    fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
    fontSize: 13,
    lineHeight: 1.1,
    // herdr owns scrollback; the browser keeps none of its own.
    scrollback: 0,
    macOptionIsMeta: true,
    // On a Mac xterm.js selects the word under a right-click even while
    // herdr tracks the mouse; the click is herdr's, not a selection.
    rightClickSelectsWord: false,
    theme: herdrTerminalTheme,
  })
  const fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.attachCustomKeyEventHandler((event) => !ownKey(event))
  terminal.open(host)
  const noContextMenu = (event: MouseEvent) => event.preventDefault()
  host.addEventListener('contextmenu', noContextMenu)
  const latin1 = (data: string) => Uint8Array.from(data, (char) => char.charCodeAt(0) & 0xff)
  return {
    kind: 'xterm',
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
    screen: () => {
      const buffer = terminal.buffer.active
      const rows = Array.from({ length: terminal.rows }, (_, row) => buffer.getLine(buffer.viewportY + row)?.translateToString(true) ?? '')
      return { cols: terminal.cols, rows, cursor: { col: buffer.cursorX, row: buffer.cursorY } }
    },
    dispose: () => {
      host.removeEventListener('contextmenu', noContextMenu)
      terminal.dispose()
    },
  }
}
