import { FitAddon, init, Terminal } from 'ghostty-web'
import { herdrTerminalTheme } from './herdrViewModel.ts'
import { cellAt, metaKey, mouseModes, mouseReport, wantsMove, type MouseAction } from './terminalInput.ts'
import type { TerminalSurface, TerminalSurfaceOptions } from './terminalSurface.ts'

// The parts of ghostty-web 0.4.0's terminal, renderer and selection the
// render scheduling, the caret and the right button need; none are in its
// typings, so each is checked before use.
type Painter = {
  ctx: CanvasRenderingContext2D
  metrics: { width: number, height: number, baseline: number }
  render: (buffer: object, forceAll: boolean, viewportY: number, scrollback: object, scrollbarOpacity: number) => void
  renderCursor: (col: number, row: number) => void
  startCursorBlink: () => void
  stopCursorBlink: () => void
  cursorVisible: boolean
  cursorBlinkInterval?: number
  setHoveredHyperlinkId: (id: unknown) => void
  setHoveredLinkRange: (range: unknown) => void
}
type Internals = {
  renderer?: Partial<Painter>
  selectionManager?: { boundContextMenuHandler?: (event: MouseEvent) => void, requestRender?: () => void }
  canvas?: HTMLCanvasElement
  wasmTerm?: object
  animationFrameId?: number
  startRenderLoop?: () => void
  isDisposed?: boolean
  viewportY?: number
  scrollbarOpacity?: number
}

// Renders is what the surface asks of the scheduler: a render next frame,
// shown or hidden, and how many renders there have been.
type Renders = { request(): void, setVisible(visible: boolean): void, renders(): number, dispose(): void }

const fontFamily = '"SFMono-Regular", Consolas, "Liberation Mono", monospace'
const fontSize = 13

let loading: Promise<void> | undefined

// openGhosttySurface is the herdr view's terminal on libghostty. It loads
// the WASM (once per page; it is inlined in this module's chunk), and
// throws when that fails so the view can fall back to xterm.js.
export async function openGhosttySurface(host: HTMLElement, { ownKey }: TerminalSurfaceOptions): Promise<TerminalSurface> {
  // One load for the page, however many views ask at once.
  loading ??= init()
  await loading
  const terminal = new Terminal({
    cursorBlink: true,
    cursorStyle: 'block',
    fontFamily,
    fontSize,
    // herdr owns scrollback; the browser keeps none of its own.
    scrollback: 0,
    theme: herdrTerminalTheme,
  })
  const listeners = new Set<(data: string | Uint8Array) => void>()
  const send = (data: string | Uint8Array) => { for (const listener of listeners) listener(data) }
  // ghostty-web's handler answers "handled" (true), the opposite of xterm.js's.
  terminal.attachCustomKeyEventHandler((event) => {
    if (ownKey(event)) return true
    // Option as Meta: ⌥ and a key is ESC and the key, on a Mac too.
    const meta = metaKey(event)
    if (meta === null) return false
    send(meta)
    return true
  })
  const fit = new FitAddon()
  const element = document.createElement('div')
  element.className = 'herdr-ghostty'
  host.append(element)
  try {
    terminal.loadAddon(fit)
    terminal.open(element)
  } catch (error) {
    element.remove()
    throw error
  }
  const internals = terminal as unknown as Internals
  const renders = renderOnDemand(terminal, internals)
  const disposeCaret = paintCaret(terminal, internals.renderer, element, renders.request)
  const disposeMouse = reportMouse(terminal, internals, host, element, send)
  const text = terminal.onData((data) => send(data))

  return {
    kind: 'ghostty',
    write: (bytes) => {
      terminal.write(bytes)
      renders.request()
    },
    onInput: (listener) => {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
    fit: () => {
      const proposal = fit.proposeDimensions()
      if (!proposal || !Number.isFinite(proposal.cols) || !Number.isFinite(proposal.rows)) return null
      const cols = Math.max(2, proposal.cols)
      const rows = Math.max(2, proposal.rows)
      if (terminal.cols !== cols || terminal.rows !== rows) terminal.resize(cols, rows)
      return { cols, rows }
    },
    focus: () => terminal.focus(),
    // A full reset (RIS) in the parser: ghostty-web's own reset() frees the
    // WASM terminal while its selection still reads from it.
    reset: () => {
      terminal.write('\x1bc')
      renders.request()
    },
    setVisible: renders.setVisible,
    renders: renders.renders,
    screen: () => {
      const buffer = terminal.buffer.active
      const rows = Array.from({ length: terminal.rows }, (_, row) => visibleLine(terminal, row)?.translateToString(true) ?? '')
      return { cols: terminal.cols, rows, cursor: { col: buffer.cursorX, row: buffer.cursorY } }
    },
    dispose: () => {
      text.dispose()
      disposeMouse()
      disposeCaret()
      renders.dispose()
      listeners.clear()
      terminal.dispose()
      element.remove()
    },
  }
}

// visibleLine is a row of the screen: ghostty-web's buffer counts any
// scrollback first and its viewportY is always 0.
function visibleLine(terminal: Terminal, row: number) {
  const buffer = terminal.buffer.active
  return buffer.getLine(buffer.length - terminal.rows + row)
}

// The renderer's own blink period.
const blinkMs = 530

// renderOnDemand replaces ghostty-web 0.4.0's render loop, which renders
// every animation frame for as long as the terminal is open, hidden or not
// (it held the owner's tab near 80% CPU), with at most one render a frame
// and only when something changed: a write (the surface asks), a resize, a
// scroll, a selection, a hovered link, focus (paintCaret asks) or a caret
// blink. The caret blinks only while the renderer is asked to blink (paintCaret
// asks while focused) and the terminal is shown. Hidden, it renders nothing
// and keeps no timer; shown again, one full render brings the screen back.
// Should a release drop any internal this needs, the stock loop stands, and
// this is the one place that says so.
function renderOnDemand(terminal: Terminal, internals: Internals): Renders {
  const painter = internals.renderer
  const buffer = internals.wasmTerm
  let count = 0
  // Counted either way, so the tests can see the stock loop too.
  if (typeof painter?.render === 'function') {
    const render = painter.render.bind(painter)
    painter.render = (...args) => { count++; render(...args) }
  }
  if (!painter || !buffer || typeof painter.render !== 'function' || typeof internals.animationFrameId !== 'number' || typeof internals.startRenderLoop !== 'function'
    || typeof painter.startCursorBlink !== 'function' || typeof painter.stopCursorBlink !== 'function' || typeof painter.cursorVisible !== 'boolean') {
    console.warn('herdr view: ghostty-web has changed inside, so it renders every frame as it ships')
    return { request: () => {}, setVisible: () => {}, renders: () => count, dispose: () => {} }
  }
  const render = painter.render
  let shown = true
  let full = false
  let frame: number | undefined
  const paint = () => {
    // Cleared first: a render that throws must not stop the next one.
    frame = undefined
    if (!shown || internals.isDisposed) return
    const forceAll = full
    full = false
    render(buffer, forceAll, internals.viewportY ?? 0, terminal, internals.scrollbarOpacity ?? 0)
  }
  const request = () => {
    if (shown && frame === undefined) frame = requestAnimationFrame(paint)
  }
  // open() started the loop; its next frame is cancelled, and a restart asks for one frame.
  cancelAnimationFrame(internals.animationFrameId)
  internals.animationFrameId = undefined
  internals.startRenderLoop = request

  // The blink: the renderer's own timer only flips cursorVisible for the
  // loop to paint, so it is replaced by one that asks for the frame.
  const stockStop = painter.stopCursorBlink.bind(painter)
  let blinking = painter.cursorBlinkInterval !== undefined
  let blinkTimer: ReturnType<typeof setInterval> | undefined
  stockStop()
  const blink = () => {
    const run = blinking && shown
    if (run && blinkTimer === undefined) {
      blinkTimer = setInterval(() => {
        painter.cursorVisible = !painter.cursorVisible
        request()
      }, blinkMs)
    } else if (!run && blinkTimer !== undefined) {
      clearInterval(blinkTimer)
      blinkTimer = undefined
      painter.cursorVisible = true
    }
  }
  painter.startCursorBlink = () => {
    blinking = true
    blink()
  }
  painter.stopCursorBlink = () => {
    stockStop()
    blinking = false
    blink()
  }
  blink()

  // The selection and the link hover change the screen without a write.
  if (internals.selectionManager && typeof internals.selectionManager.requestRender === 'function') internals.selectionManager.requestRender = request
  for (const name of ['setHoveredHyperlinkId', 'setHoveredLinkRange'] as const) {
    const set = painter[name]
    if (typeof set !== 'function') continue
    painter[name] = (value: unknown) => {
      set.call(painter, value)
      request()
    }
  }
  const scrolled = terminal.onScroll(request)
  const resized = terminal.onResize(request)

  return {
    request,
    setVisible: (visible) => {
      if (visible === shown) return
      shown = visible
      if (!shown && frame !== undefined) {
        cancelAnimationFrame(frame)
        frame = undefined
      }
      blink()
      if (shown) {
        full = true
        request()
      }
    },
    renders: () => count,
    dispose: () => {
      if (frame !== undefined) cancelAnimationFrame(frame)
      frame = undefined
      shown = false
      blink()
      scrolled.dispose()
      resized.dispose()
    },
  }
}

// paintCaret draws the caret as the herdr view's theme says: a solid block
// with the character under it in the accent colour while the terminal has
// the keys, a steady outline once focus leaves. ghostty-web 0.4.0 paints a
// plain block over the character and ignores focus.
function paintCaret(terminal: Terminal, painter: Partial<Painter> | undefined, element: HTMLElement, request: () => void) {
  if (!painter?.ctx || !painter.metrics || typeof painter.renderCursor !== 'function' || typeof painter.startCursorBlink !== 'function' || typeof painter.stopCursorBlink !== 'function') return () => {}
  const { ctx } = painter
  const block = painter.renderCursor.bind(painter)
  const blink = painter.startCursorBlink.bind(painter)
  const steady = painter.stopCursorBlink.bind(painter)
  let focused = element.contains(document.activeElement)
  painter.renderCursor = (col, row) => {
    const metrics = painter.metrics!
    const x = col * metrics.width
    const y = row * metrics.height
    if (!focused) {
      ctx.strokeStyle = herdrTerminalTheme.cursor
      ctx.lineWidth = 1
      ctx.strokeRect(x + 0.5, y + 0.5, metrics.width - 1, metrics.height - 1)
      return
    }
    block(col, row)
    const char = visibleLine(terminal, row)?.getCell(col)?.getChars()
    if (!char || char === ' ') return
    ctx.fillStyle = herdrTerminalTheme.cursorAccent
    ctx.font = `${fontSize}px ${fontFamily}`
    ctx.fillText(char, x, y + metrics.baseline)
  }
  // The renderer repaints the caret's row on every render while blinking,
  // so the outline replaces the block; out of focus it stays lit.
  const onFocus = () => { focused = true; steady(); blink(); request() }
  const onBlur = () => { focused = false; steady(); request() }
  if (!focused) steady()
  element.addEventListener('focusin', onFocus)
  element.addEventListener('focusout', onBlur)
  return () => {
    element.removeEventListener('focusin', onFocus)
    element.removeEventListener('focusout', onBlur)
  }
}

// reportMouse sends the client the mouse reports it asked for (ghostty-web
// 0.4.0 tracks the modes but reports nothing). While the client tracks the
// mouse a press is its own, not a selection; ⇧ still selects, as in
// xterm.js. A right-click never opens the browser's menu on the terminal.
function reportMouse(terminal: Terminal, internals: Internals, host: HTMLElement, element: HTMLElement, send: (data: string | Uint8Array) => void) {
  const modes = () => mouseModes((mode) => terminal.getMode(mode))
  const at = (event: MouseEvent) => {
    const canvas = internals.canvas ?? element
    return {
      ...cellAt({ x: event.clientX, y: event.clientY }, canvas.getBoundingClientRect(), { cols: terminal.cols, rows: terminal.rows }),
      shift: event.shiftKey, alt: event.altKey, ctrl: event.ctrlKey,
    }
  }
  const report = (action: MouseAction, event: MouseEvent) => {
    const bytes = mouseReport(modes(), action, at(event))
    if (bytes !== null) send(bytes)
  }
  const held = new Set<number>()
  let last = ''
  const onDown = (event: MouseEvent) => {
    if (modes().tracking === 'off' || event.shiftKey) return
    event.preventDefault()
    event.stopPropagation()
    terminal.focus()
    held.add(event.button)
    report({ kind: 'press', button: event.button }, event)
  }
  const onUp = (event: MouseEvent) => {
    if (!held.delete(event.button)) return
    report({ kind: 'release', button: event.button }, event)
  }
  const onMove = (event: MouseEvent) => {
    const button = held.size > 0 ? [...held][0] : null
    if (!wantsMove(modes(), button !== null)) return
    const cell = at(event)
    const key = `${cell.col};${cell.row};${button}`
    if (key === last) return
    last = key
    report({ kind: 'move', button }, event)
  }
  terminal.attachCustomWheelEventHandler((event) => {
    if (modes().tracking === 'off' || event.deltaY === 0) return false
    report({ kind: 'wheel', direction: event.deltaY < 0 ? 'up' : 'down' }, event)
    return true
  })
  const noContextMenu = (event: MouseEvent) => event.preventDefault()
  // ghostty-web answers a right-click by moving its textarea under the
  // pointer for the browser's menu; the button is herdr's.
  const menuHandler = internals.selectionManager?.boundContextMenuHandler
  if (menuHandler && internals.canvas) internals.canvas.removeEventListener('contextmenu', menuHandler)
  host.addEventListener('contextmenu', noContextMenu)
  host.addEventListener('mousedown', onDown, { capture: true })
  host.addEventListener('mousemove', onMove)
  const onBlur = () => held.clear()
  window.addEventListener('mouseup', onUp)
  window.addEventListener('blur', onBlur)
  return () => {
    window.removeEventListener('blur', onBlur)
    host.removeEventListener('contextmenu', noContextMenu)
    host.removeEventListener('mousedown', onDown, { capture: true })
    host.removeEventListener('mousemove', onMove)
    window.removeEventListener('mouseup', onUp)
  }
}
