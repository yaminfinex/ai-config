export const terminalReset = '\x1bc'
const terminalResetAttributes = '\x1b[0m'
const terminalEraseLine = '\x1b[2K'
const terminalSaveCursor = '\x1b7'
const terminalRestoreCursor = '\x1b8'

export function snapshotRepaint(snapshot: string) {
  return terminalReset + snapshot
}

export function snapshotUpdate(previous: string, snapshot: string) {
  if (previous === snapshot) return null
  const previousLines = previous.split('\n')
  const nextLines = snapshot.split('\n')
  if (previousLines.length !== nextLines.length) return snapshotRepaint(snapshot)
  const changedRows = nextLines.flatMap((line, index) => line === previousLines[index] ? [] : [index])
  if (changedRows.length * 2 > nextLines.length) return snapshotRepaint(snapshot)
  return terminalSaveCursor
    + changedRows.map((index) => `\x1b[${index + 1};1H${terminalResetAttributes}${terminalEraseLine}${nextLines[index]}`).join('')
    + terminalRestoreCursor
}

export class SnapshotPainter {
  private queued: string | null = null
  private writing = false
  private painted: string | null = null
  private active = true
  private baselineVersion = 0
  private readonly write: (data: string, done: () => void) => void
  private readonly incremental: boolean

  constructor(write: (data: string, done: () => void) => void, incremental = true) {
    this.write = write
    this.incremental = incremental
  }

  get pending() { return this.writing || this.queued !== null }

  reset() {
    this.painted = null
    this.baselineVersion += 1
  }

  paint(snapshot: string, active = true) {
    this.active = active
    if (!active) {
      this.queued = null
      return
    }
    this.queued = snapshot.endsWith('\n') ? snapshot.slice(0, -1) : snapshot
    this.drain()
  }

  private drain() {
    if (this.writing || !this.active || this.queued === null) return
    const snapshot = this.queued
    this.queued = null
    const update = this.painted === null || !this.incremental ? snapshotRepaint(snapshot) : snapshotUpdate(this.painted, snapshot)
    if (update === null) {
      this.painted = snapshot
      this.drain()
      return
    }
    this.writing = true
    const baselineVersion = this.baselineVersion
    this.write(update, () => {
      if (baselineVersion === this.baselineVersion) this.painted = snapshot
      this.writing = false
      this.drain()
    })
  }
}
