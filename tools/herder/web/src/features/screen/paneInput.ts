export type PaneInput = { text: string } | { keys: string[] }

const keyChunks = new Map<string, string>([
  ['\r', 'enter'],
  ['\x7f', 'backspace'],
  ['\t', 'tab'],
  ['\x03', 'ctrl+c'],
  ['\x04', 'ctrl+d'],
  ['\x1b', 'escape'],
  ['\x1b[A', 'up'],
  ['\x1b[B', 'down'],
  ['\x1b[C', 'right'],
  ['\x1b[D', 'left'],
  ['\x1bb', 'alt+b'],
  ['\x1bf', 'alt+f'],
])

export function encodeTerminalInput(chunk: string): PaneInput {
  const key = keyChunks.get(chunk)
  return key ? { keys: [key] } : { text: chunk }
}

export class PaneInputQueue {
  private tail = Promise.resolve()
  private readonly write: (input: PaneInput) => Promise<void>

  constructor(write: (input: PaneInput) => Promise<void>) {
    this.write = write
  }

  send(chunk: string) {
    const request = this.tail.then(() => this.write(encodeTerminalInput(chunk)))
    this.tail = request.catch(() => undefined)
    return request
  }
}
