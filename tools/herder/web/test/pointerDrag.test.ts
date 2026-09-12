import assert from 'node:assert/strict'
import test from 'node:test'

class FakeTarget extends EventTarget {
  captured: number[] = []
  released: number[] = []
  setPointerCapture(id: number) { this.captured.push(id) }
  releasePointerCapture(id: number) { this.released.push(id) }
}

class CountingWindow extends EventTarget {
  listeners = new Map<string, number>()
  override addEventListener(type: string, listener: EventListenerOrEventListenerObject | null, options?: boolean | AddEventListenerOptions) {
    this.listeners.set(type, (this.listeners.get(type) ?? 0) + 1)
    super.addEventListener(type, listener, options)
  }
  override removeEventListener(type: string, listener: EventListenerOrEventListenerObject | null, options?: boolean | EventListenerOptions) {
    this.listeners.set(type, (this.listeners.get(type) ?? 0) - 1)
    super.removeEventListener(type, listener, options)
  }
  total() { return [...this.listeners.values()].reduce((sum, count) => sum + count, 0) }
  pointer(type: string, pointerId: number, clientX = 0) {
    const event = new Event(type) as Event & { pointerId: number, clientX: number }
    event.pointerId = pointerId
    event.clientX = clientX
    this.dispatchEvent(event)
  }
}

async function withWindow<T>(run: (win: CountingWindow) => Promise<T> | T) {
  const win = new CountingWindow()
  const previous = (globalThis as { window?: unknown }).window
  ;(globalThis as { window?: unknown }).window = win
  try { return await run(win) } finally { (globalThis as { window?: unknown }).window = previous }
}

test('startPointerDrag captures the pointer, follows only that pointer, and pointercancel ends the drag like pointerup', async () => {
  await withWindow(async (win) => {
    const { startPointerDrag } = await import('../src/shared/lifecycle.ts')
    const target = new FakeTarget()
    const moves: number[] = []
    startPointerDrag({ pointerId: 7, currentTarget: target }, (event) => moves.push(event.clientX))
    assert.deepEqual(target.captured, [7])
    assert.equal(win.total(), 3)
    win.pointer('pointermove', 7, 10)
    win.pointer('pointermove', 9, 99)
    assert.deepEqual(moves, [10])
    win.pointer('pointercancel', 9)
    assert.equal(win.total(), 3, 'a different pointer does not end the drag')
    win.pointer('pointercancel', 7)
    assert.equal(win.total(), 0)
    assert.deepEqual(target.released, [7])
    win.pointer('pointermove', 7, 20)
    assert.deepEqual(moves, [10], 'moves after a cancel are ignored')
  })
})

test('a second start disposes the first and the returned disposer removes every listener', async () => {
  await withWindow(async (win) => {
    const { startPointerDrag } = await import('../src/shared/lifecycle.ts')
    const target = new FakeTarget()
    const first: number[] = []
    const second: number[] = []
    let active = startPointerDrag({ pointerId: 1, currentTarget: target }, (event) => first.push(event.clientX))
    active()
    active = startPointerDrag({ pointerId: 2, currentTarget: target }, (event) => second.push(event.clientX))
    assert.equal(win.total(), 3, 'only the second drag is listening')
    win.pointer('pointermove', 1, 5)
    win.pointer('pointermove', 2, 6)
    assert.deepEqual(first, [])
    assert.deepEqual(second, [6])
    active()
    active()
    assert.equal(win.total(), 0, 'unmount-style disposal removes all listeners once')
    assert.deepEqual(target.released, [1, 2])
    win.pointer('pointerup', 2)
    assert.equal(win.total(), 0)
  })
})
