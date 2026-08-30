import assert from 'node:assert/strict'
import test from 'node:test'
import { copyPath } from '../src/shared/pathCopyModel.ts'

test('copy path reports success only after the clipboard accepts the absolute path', async () => {
  const writes: string[] = []
  assert.equal(await copyPath({ writeText: async (value) => { writes.push(value) } }, '/repo/src/App.tsx'), 'copied')
  assert.deepEqual(writes, ['/repo/src/App.tsx'])
})

test('copy path fails honestly when clipboard access is unavailable or rejects', async () => {
  assert.equal(await copyPath(undefined, '/repo/src/App.tsx'), 'failed')
  assert.equal(await copyPath({ writeText: async () => { throw new Error('denied') } }, '/repo/src/App.tsx'), 'failed')
})
