import assert from 'node:assert/strict'
import test from 'node:test'
import { copyPath } from '../src/shared/pathCopyModel.ts'

test('copy path reports success only after the clipboard accepts the absolute path', async () => {
  const writes: string[] = []
  assert.equal(await copyPath({ writeText: async (value) => { writes.push(value) } }, '/repo/src/App.tsx'), 'copied')
  assert.deepEqual(writes, ['/repo/src/App.tsx'])
})

test('copy path falls back when the clipboard writer is unavailable or rejects', async () => {
  const legacyWrites: string[] = []
  const execCopy = (value: string) => { legacyWrites.push(value); return true }
  assert.equal(await copyPath(undefined, '/repo/src/App.tsx', execCopy), 'copied')
  assert.equal(await copyPath({ writeText: async () => { throw new Error('denied') } }, '/repo/src/App.tsx', execCopy), 'copied')
  assert.deepEqual(legacyWrites, ['/repo/src/App.tsx', '/repo/src/App.tsx'])
})

test('copy path fails honestly only when the clipboard writer and fallback both fail', async () => {
  assert.equal(await copyPath(undefined, '/repo/src/App.tsx', () => false), 'failed')
  assert.equal(await copyPath({ writeText: async () => { throw new Error('denied') } }, '/repo/src/App.tsx', () => false), 'failed')
})
