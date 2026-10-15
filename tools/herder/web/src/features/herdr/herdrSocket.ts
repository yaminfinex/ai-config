import { focusFrame, leaveGraceMs, parseNotice, reconnectDelay, resizeFrame, type HerdrConnection, type HerdrNotice } from './herdrViewModel.ts'

type SocketLike = Pick<WebSocket, 'readyState' | 'send' | 'close' | 'binaryType'> & {
  onopen: ((event: Event) => void) | null
  onmessage: ((event: MessageEvent) => void) | null
  onclose: ((event: CloseEvent) => void) | null
  onerror: ((event: Event) => void) | null
}

export type HerdrSocketOptions = {
  url: (size: { cols: number, rows: number } | undefined) => string
  onOutput: (bytes: Uint8Array) => void
  onNotice: (notice: HerdrNotice) => void
  onConnection: (connection: HerdrConnection, retryInMs?: number) => void
  createSocket?: (url: string) => SocketLike
  setTimer?: (callback: () => void, ms: number) => unknown
  clearTimer?: (timer: unknown) => void
}

const OPEN = 1

// HerdrSocket is the one herdr client a browser keeps: attach opens it (or
// cancels a pending leave), detach closes it after a grace, a drop that the
// owner did not ask for reconnects with backoff, and a client that exited
// by itself waits for reconnect() or the next attach.
export class HerdrSocket {
  private socket: SocketLike | null = null
  private attempt = 0
  private retryTimer: unknown = null
  private graceTimer: unknown = null
  private size: { cols: number, rows: number } | undefined
  private connection: HerdrConnection = 'idle'
  private exited = false
  private disposed = false
  private readonly options: Required<HerdrSocketOptions>
  private readonly encoder = new TextEncoder()

  constructor(options: HerdrSocketOptions) {
    this.options = {
      createSocket: (url) => new WebSocket(url),
      setTimer: (callback, ms) => setTimeout(callback, ms),
      clearTimer: (timer) => clearTimeout(timer as ReturnType<typeof setTimeout>),
      ...options,
    }
  }

  get state() { return this.connection }

  // attach opens the client, or keeps the live one; coming back to a client
  // that exited starts a new one.
  attach() {
    this.cancelGrace()
    if (this.disposed || this.socket || this.retryTimer !== null) return
    this.attempt = 0
    this.open()
  }

  // detach keeps the client for the grace, then lets it go.
  detach() {
    if (this.disposed || this.graceTimer !== null) return
    this.graceTimer = this.options.setTimer(() => {
      this.graceTimer = null
      this.close('idle')
    }, leaveGraceMs)
  }

  reconnect() {
    this.close('idle')
    this.attempt = 0
    this.open()
  }

  input(data: string | Uint8Array) {
    if (this.socket?.readyState !== OPEN) return false
    this.socket.send(typeof data === 'string' ? this.encoder.encode(data) : data)
    return true
  }

  resize(cols: number, rows: number) {
    if (this.size?.cols === cols && this.size.rows === rows) return
    this.size = { cols, rows }
    if (this.socket?.readyState === OPEN) this.socket.send(resizeFrame(cols, rows))
  }

  focus(paneID: string) {
    if (this.socket?.readyState !== OPEN) return false
    this.socket.send(focusFrame(paneID))
    return true
  }

  dispose() {
    this.disposed = true
    this.close('idle')
  }

  private open() {
    this.exited = false
    this.set(this.attempt === 0 ? 'connecting' : 'reconnecting')
    const socket = this.options.createSocket(this.options.url(this.size))
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    socket.onopen = () => {
      if (this.socket !== socket) return
      this.attempt = 0
      // The URL carried the size when it was known; send it again in case
      // the view resized while the socket was opening.
      if (this.size) socket.send(resizeFrame(this.size.cols, this.size.rows))
      this.set('live')
    }
    socket.onmessage = (event) => {
      if (this.socket !== socket) return
      if (typeof event.data === 'string') {
        const notice = parseNotice(event.data)
        if (!notice) return
        if (notice.type === 'exit') this.exited = true
        this.options.onNotice(notice)
        return
      }
      this.options.onOutput(new Uint8Array(event.data as ArrayBuffer))
    }
    socket.onerror = () => undefined
    socket.onclose = () => {
      if (this.socket !== socket) return
      this.socket = null
      if (this.exited) {
        this.set('ended')
        return
      }
      const delay = reconnectDelay(this.attempt)
      this.attempt++
      this.set('reconnecting', delay)
      this.retryTimer = this.options.setTimer(() => {
        this.retryTimer = null
        this.open()
      }, delay)
    }
  }

  private close(next: HerdrConnection) {
    this.cancelGrace()
    if (this.retryTimer !== null) {
      this.options.clearTimer(this.retryTimer)
      this.retryTimer = null
    }
    const socket = this.socket
    this.socket = null
    if (socket) {
      socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null
      socket.close(1000, 'left the herdr view')
    }
    this.set(next)
  }

  private cancelGrace() {
    if (this.graceTimer === null) return
    this.options.clearTimer(this.graceTimer)
    this.graceTimer = null
  }

  private set(connection: HerdrConnection, retryInMs?: number) {
    this.connection = connection
    if (!this.disposed) this.options.onConnection(connection, retryInMs)
  }
}
