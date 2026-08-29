export const terminalReset = '\x1bc'

export function snapshotRepaint(snapshot: string) {
  return terminalReset + snapshot
}

export class SnapshotPainter {
  private queued: string | null = null
  private writing = false
  private readonly write: (data: string, done: () => void) => void

  constructor(write: (data: string, done: () => void) => void) {
    this.write = write
  }

  get pending() { return this.writing || this.queued !== null }

  paint(snapshot: string) {
    this.queued = snapshot
    this.drain()
  }

  private drain() {
    if (this.writing || this.queued === null) return
    const snapshot = this.queued
    this.queued = null
    this.writing = true
    this.write(snapshotRepaint(snapshot), () => {
      this.writing = false
      this.drain()
    })
  }
}
