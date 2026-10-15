import assert from 'node:assert/strict'
import test from 'node:test'
import { HerdrSocket } from '../src/features/herdr/herdrSocket.ts'
import type { HerdrConnection, HerdrNotice } from '../src/features/herdr/herdrViewModel.ts'

class FakeSocket {
  readyState = 0
  binaryType = 'blob'
  sent: (string | Uint8Array)[] = []
  closed: number | null = null
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  readonly url: string
  constructor(url: string) { this.url = url }
  send(data: string | Uint8Array) { this.sent.push(data) }
  close(code: number) { this.closed = code; this.readyState = 3 }
  open() { this.readyState = 1; this.onopen?.(new Event('open')) }
  text(data: string) { this.onmessage?.({ data } as MessageEvent) }
  bytes(data: number[]) { this.onmessage?.({ data: new Uint8Array(data).buffer } as MessageEvent) }
  drop() { this.readyState = 3; this.onclose?.({} as CloseEvent) }
}

function harness() {
  const sockets: FakeSocket[] = []
  const timers = new Map<number, { callback: () => void, ms: number }>()
  let nextTimer = 0
  const output: number[][] = []
  const notices: HerdrNotice[] = []
  const states: [HerdrConnection, number | undefined][] = []
  const client = new HerdrSocket({
    url: (size) => `ws://serve/api/herdr/term${size ? `?cols=${size.cols}&rows=${size.rows}` : ''}`,
    onOutput: (bytes) => output.push([...bytes]),
    onNotice: (notice) => notices.push(notice),
    onConnection: (connection, retryIn) => states.push([connection, retryIn]),
    createSocket: (url) => { const socket = new FakeSocket(url); sockets.push(socket); return socket },
    setTimer: (callback, ms) => { timers.set(++nextTimer, { callback, ms }); return nextTimer },
    clearTimer: (timer) => { timers.delete(timer as number) },
  })
  const fire = (ms: number) => {
    const [id, timer] = [...timers.entries()].find(([, entry]) => entry.ms === ms) ?? []
    assert.ok(id && timer, `a ${ms} ms timer must be pending`)
    timers.delete(id)
    timer.callback()
  }
  return { client, sockets, timers, output, notices, states, fire }
}

const decode = (data: string | Uint8Array) => typeof data === 'string' ? data : new TextDecoder().decode(data)

test('attach opens one client at the known size; keys go as bytes, resizes and focus as control', () => {
  const { client, sockets, output, states } = harness()
  client.resize(120, 40)
  client.attach()
  client.attach()
  assert.equal(sockets.length, 1, 'one client per browser')
  assert.equal(sockets[0]!.url, 'ws://serve/api/herdr/term?cols=120&rows=40')
  assert.equal(sockets[0]!.binaryType, 'arraybuffer')
  assert.equal(client.input('x'), false, 'nothing is sent before the socket opens')
  sockets[0]!.open()
  assert.deepEqual(states.map(([state]) => state), ['connecting', 'live'])
  assert.deepEqual(sockets[0]!.sent.map(decode), ['{"type":"resize","cols":120,"rows":40}'])
  assert.equal(client.input('n\x1b[A'), true)
  assert.ok(sockets[0]!.sent.at(-1) instanceof Uint8Array, 'input is a binary frame')
  assert.equal(decode(sockets[0]!.sent.at(-1)!), 'n\x1b[A')
  client.resize(120, 40)
  client.resize(100, 30)
  assert.equal(client.focus('w1:p2'), true)
  assert.deepEqual(sockets[0]!.sent.slice(2).map(decode), ['{"type":"resize","cols":100,"rows":30}', '{"type":"focus","pane_id":"w1:p2"}'], 'an unchanged size is not resent')
  sockets[0]!.bytes([0, 255, 27])
  assert.deepEqual(output, [[0, 255, 27]])
})

test('a dropped socket reconnects with backoff and resets the backoff once live', () => {
  const { client, sockets, states, fire } = harness()
  client.attach()
  sockets[0]!.open()
  sockets[0]!.drop()
  assert.deepEqual(states.at(-1), ['reconnecting', 500])
  fire(500)
  assert.equal(sockets.length, 2)
  sockets[1]!.drop()
  assert.deepEqual(states.at(-1), ['reconnecting', 1000])
  fire(1000)
  sockets[2]!.open()
  assert.equal(states.at(-1)?.[0], 'live')
  sockets[2]!.drop()
  assert.deepEqual(states.at(-1), ['reconnecting', 500], 'a live socket resets the backoff')
})

test('a client that exited by itself waits for the owner; attach or reconnect starts a new one', () => {
  const { client, sockets, states, notices, timers } = harness()
  client.attach()
  sockets[0]!.open()
  sockets[0]!.text('{"type":"exit","code":0}')
  sockets[0]!.drop()
  assert.equal(states.at(-1)?.[0], 'ended')
  assert.equal(timers.size, 0, 'no reconnect is scheduled')
  assert.deepEqual(notices, [{ type: 'exit', code: 0 }])
  client.attach()
  assert.equal(sockets.length, 2)
  client.reconnect()
  assert.equal(sockets[1]!.closed, 1000)
  assert.equal(sockets.length, 3)
})

test('leaving keeps the client for the grace; coming back within it keeps the same client', () => {
  const { client, sockets, timers, fire, states } = harness()
  client.attach()
  sockets[0]!.open()
  client.detach()
  assert.equal(timers.size, 1)
  client.attach()
  assert.equal(timers.size, 0, 'coming back cancels the leave')
  assert.equal(sockets.length, 1)
  assert.equal(sockets[0]!.closed, null)
  client.detach()
  fire(60_000)
  assert.equal(sockets[0]!.closed, 1000, 'the client goes after the grace')
  assert.equal(states.at(-1)?.[0], 'idle')
  sockets[0]!.drop()
  assert.equal(timers.size, 0, 'a close we asked for never reconnects')
  client.attach()
  assert.equal(sockets.length, 2)
  client.dispose()
  assert.equal(sockets[1]!.closed, 1000)
  client.attach()
  assert.equal(sockets.length, 2, 'a disposed socket stays closed')
})
