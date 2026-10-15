import { spawn, type ChildProcess } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import type { Socket } from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import type { Writable } from 'node:stream'
import type { TestContext } from 'node:test'

import type { Plugin } from 'vite'

const fixtures = new URL('./fixtures/herdr-term/', import.meta.url).pathname

// herdrTermFixture stands in for the serve's GET /api/herdr/term: each
// WebSocket runs `sh -c command` in a real PTY at the page's cols and rows,
// as the serve does under HERDER_TERM_COMMAND (pty_relay.py). Binary frames
// are bytes both ways; a resize text frame resizes the PTY and a focus one
// is answered "focused". By default the command is fake_herdr.py, which
// logs each size it takes and each input chunk; log() reads that back.
export async function herdrTermFixture(context: TestContext, command?: string) {
  const dir = await mkdtemp(join(tmpdir(), 'herder-herdr-term-'))
  const logPath = join(dir, 'client.log')
  const run = command ?? `exec python3 -I ${fixtures}fake_herdr.py ${logPath}`
  const relays = new Set<ChildProcess>()
  const controls: string[] = []
  const hangUp = (relay: ChildProcess) => { try { process.kill(-relay.pid!, 'SIGHUP') } catch { /* already gone */ } }
  context.after(async () => {
    for (const relay of relays) hangUp(relay)
    await rm(dir, { recursive: true, force: true })
  })
  const plugin: Plugin = {
    name: 'herder-fixture-herdr-term',
    configureServer(server) {
      server.httpServer?.on('upgrade', (request, socket: Socket) => {
        const url = new URL(request.url ?? '/', 'http://fixture')
        if (url.pathname !== '/api/herdr/term') return
        const accept = createHash('sha1').update(`${request.headers['sec-websocket-key']}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`).digest('base64')
        socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`)
        const relay = spawn('python3', ['-I', `${fixtures}pty_relay.py`, url.searchParams.get('cols') ?? '80', url.searchParams.get('rows') ?? '24', run], {
          detached: true, stdio: ['pipe', 'pipe', 'inherit', 'pipe'],
        })
        relays.add(relay)
        const control = relay.stdio[3] as Writable
        const send = (opcode: number, payload: Buffer) => {
          if (socket.writable) socket.write(Buffer.concat([frameHeader(opcode, payload.length), payload]))
        }
        relay.stdout!.on('data', (chunk: Buffer) => send(2, chunk))
        relay.on('exit', (code) => {
          relays.delete(relay)
          send(1, Buffer.from(JSON.stringify({ type: 'exit', code })))
          send(8, Buffer.alloc(0))
          socket.end()
        })
        let pending = Buffer.alloc(0)
        socket.on('data', (chunk: Buffer) => {
          pending = Buffer.concat([pending, chunk])
          for (let frame = readFrame(pending); frame; frame = readFrame(pending)) {
            pending = pending.subarray(frame.size)
            if (frame.opcode === 2) relay.stdin!.write(frame.payload)
            else if (frame.opcode === 1) {
              const text = frame.payload.toString('utf8')
              controls.push(text)
              const value = JSON.parse(text) as { type: string, cols?: number, rows?: number, pane_id?: string }
              if (value.type === 'resize') control.write(`${value.cols} ${value.rows}\n`)
              if (value.type === 'focus') send(1, Buffer.from(JSON.stringify({ type: 'focused', pane_id: value.pane_id })))
            } else if (frame.opcode === 8) socket.end()
          }
        })
        socket.on('error', () => hangUp(relay))
        socket.on('close', () => hangUp(relay))
      })
    },
  }
  // log is the client's lines so far: "size COLS ROWS" and "in HEX".
  const log = async () => (await readFile(logPath, 'utf8').catch(() => '')).split('\n').filter(Boolean)
  const typed = async () => (await log()).filter((line) => line.startsWith('in ')).map((line) => Buffer.from(line.slice(3), 'hex').toString('latin1'))
  const sizes = async () => (await log()).filter((line) => line.startsWith('size ')).map((line) => line.slice(5).split(' ').map(Number) as [number, number])
  return { plugin, log, typed, sizes, controls, open: () => relays.size }
}

function frameHeader(opcode: number, length: number) {
  if (length < 126) return Buffer.from([0x80 | opcode, length])
  if (length < 65_536) return Buffer.from([0x80 | opcode, 126, length >> 8, length & 0xff])
  const header = Buffer.alloc(10)
  header[0] = 0x80 | opcode
  header[1] = 127
  header.writeBigUInt64BE(BigInt(length), 2)
  return header
}

// readFrame takes one whole masked client frame off the front of data.
function readFrame(data: Buffer) {
  if (data.length < 2) return null
  const opcode = data[0] & 0x0f
  let length = data[1] & 0x7f
  let offset = 2
  if (length === 126) {
    if (data.length < 4) return null
    length = data.readUInt16BE(2)
    offset = 4
  } else if (length === 127) {
    if (data.length < 10) return null
    length = Number(data.readBigUInt64BE(2))
    offset = 10
  }
  const masked = (data[1] & 0x80) !== 0
  const mask = masked ? data.subarray(offset, offset + 4) : null
  if (masked) offset += 4
  if (data.length < offset + length) return null
  const payload = Buffer.from(data.subarray(offset, offset + length))
  if (mask) for (let index = 0; index < payload.length; index++) payload[index] ^= mask[index % 4]
  return { opcode, payload, size: offset + length }
}
