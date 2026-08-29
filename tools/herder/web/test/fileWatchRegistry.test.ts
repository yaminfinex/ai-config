import assert from 'node:assert/strict'
import test from 'node:test'
import { createFileWatchRegistry, type FileWatchTarget } from '../src/stream/fileWatchRegistry.ts'

test('layout-restore registrations coalesce into one ordered stream snapshot', () => {
  let nextID = 1
  const pending = new Map<number, () => void>()
  const timers = {
    setTimeout(callback: () => void) {
      const id = nextID++
      pending.set(id, callback)
      return id
    },
    clearTimeout(id: number) { pending.delete(id) },
  }
  const snapshots: FileWatchTarget[][] = []
  const registry = createFileWatchRegistry((targets) => snapshots.push(targets), timers as Pick<typeof window, 'setTimeout' | 'clearTimeout'>, 50)
  const runPending = () => {
    const entry = pending.entries().next().value as [number, () => void] | undefined
    assert.ok(entry)
    pending.delete(entry[0])
    entry[1]()
  }
  const readme = { kind: 'file' as const, root: '/repo', path: 'README.md' }
  const docs = { kind: 'folder' as const, root: '/repo', path: 'docs' }
  const release = registry.register(readme)
  registry.register(docs)
  registry.register(readme)

  assert.equal(pending.size, 1)
  runPending()
  assert.deepEqual(snapshots, [[readme, docs]])

  release()
  assert.equal(pending.size, 1)
  runPending()
  assert.equal(snapshots.length, 1, 'one remaining reference does not rebuild an unchanged stream')
  registry.dispose()
})

test('removing and reopening a target makes it newest for server eviction order', () => {
  const callbacks: Array<() => void> = []
  const timers = {
    setTimeout(callback: () => void) { callbacks.push(callback); return callbacks.length },
    clearTimeout() {},
  }
  const snapshots: FileWatchTarget[][] = []
  const registry = createFileWatchRegistry((targets) => snapshots.push(targets), timers as Pick<typeof window, 'setTimeout' | 'clearTimeout'>)
  const first = { kind: 'folder' as const, root: '/repo', path: 'first' }
  const second = { kind: 'folder' as const, root: '/repo', path: 'second' }
  const closeFirst = registry.register(first)
  registry.register(second)
  closeFirst()
  registry.register(first)
  callbacks.at(-1)?.()
  assert.deepEqual(snapshots.at(-1), [second, first])
})
