// Runs in the page: paints one burst of mixed ANSI on xterm.js or ghostty-web,
// each set up as the herdr view's surfaces are, and answers the milliseconds
// from the first write until the burst is on screen, and the screen's text.
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { init, Terminal as GhosttyTerminal } from 'ghostty-web'
import { herdrTerminalTheme } from '../../src/features/herdr/herdrViewModel.ts'

const options = { cols: 160, rows: 50, fontSize: 13, fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace', scrollback: 0, theme: herdrTerminalTheme, cursorBlink: false }
const frame = () => new Promise((resolve) => requestAnimationFrame(resolve))

// burst is size bytes of what agents print: 256-colour and truecolour
// SGR runs, bold, wide and combining characters, line clears, cursor
// moves and the odd full-screen clear, from a fixed seed.
export function burst(size: number) {
  let seed = 7
  const random = (n: number) => { seed = (seed * 1_103_515_245 + 12_345) % 2_147_483_648; return seed % n }
  const words = ['herdr', 'agent', 'compile', 'error:', 'ok', 'λ', '漢字', 'naïve', 'résumé', '✓', '→', 'packages/web/src/index.ts', '0x7f3a', 'warning', '🙂']
  const parts: string[] = []
  let length = 0
  while (length < size) {
    let line = ''
    for (let word = 0; word < 12; word++) {
      const pick = random(10)
      if (pick < 3) line += `\x1b[38;5;${random(256)}m`
      else if (pick < 5) line += `\x1b[38;2;${random(256)};${random(256)};${random(256)}m`
      else if (pick === 5) line += '\x1b[1m'
      line += `${words[random(words.length)]} `
      if (pick > 7) line += '\x1b[0m'
    }
    line += '\x1b[K\x1b[0m\r\n'
    if (random(40) === 0) line += `\x1b[${1 + random(50)};${1 + random(160)}H`
    if (random(400) === 0) line += '\x1b[2J\x1b[H'
    parts.push(line)
    length += line.length
  }
  return new TextEncoder().encode(parts.join(''))
}

function host() {
  const element = document.createElement('div')
  element.style.cssText = 'position:fixed;inset:0;width:1200px;height:800px;z-index:1000;background:#0d0f12'
  document.body.append(element)
  return element
}

// Writes go in 32 KiB chunks, as the serve reads the PTY.
const chunks = (data: Uint8Array) => Array.from({ length: Math.ceil(data.length / 32_768) }, (_, index) => data.subarray(index * 32_768, (index + 1) * 32_768))

export async function paintXterm(data: Uint8Array) {
  const element = host()
  const terminal = new XTerm(options)
  terminal.open(element)
  await frame()
  const start = performance.now()
  for (const chunk of chunks(data)) terminal.write(chunk)
  await new Promise<void>((resolve) => terminal.write('', resolve))
  await frame()
  await frame()
  const elapsed = performance.now() - start
  const text = screen(terminal)
  terminal.dispose()
  element.remove()
  return { elapsed, text }
}

export async function paintGhostty(data: Uint8Array) {
  await init()
  const element = host()
  const terminal = new GhosttyTerminal(options)
  terminal.open(element)
  await frame()
  const start = performance.now()
  for (const chunk of chunks(data)) terminal.write(chunk)
  await frame()
  await frame()
  const elapsed = performance.now() - start
  const text = screen(terminal)
  terminal.dispose()
  element.remove()
  return { elapsed, text }
}

type Lines = { rows: number, buffer: { active: { length: number, getLine(y: number): { translateToString(trim: boolean): string } | undefined } } }
const screen = (terminal: Lines) => Array.from({ length: terminal.rows }, (_, row) => terminal.buffer.active.getLine(terminal.buffer.active.length - terminal.rows + row)?.translateToString(true) ?? '').join('\n')
