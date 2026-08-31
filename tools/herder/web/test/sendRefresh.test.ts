import assert from 'node:assert/strict'
import test from 'node:test'
import { beginSendRefresh, deferMessageRefresh, settleSendRefresh } from '../src/sendRefresh.ts'

test('a successful send absorbs its deferred SSE wake into one refresh', async () => {
  const owner = {}
  const token = beginSendRefresh(owner, 'dore')
  assert.equal(deferMessageRefresh(owner, 'dore'), true)
  let refreshes = 0
  await settleSendRefresh(token, true, async () => { refreshes++ })
  assert.equal(refreshes, 1)
  assert.equal(deferMessageRefresh(owner, 'dore'), false)
})

test('a failed send replays a deferred message wake but not an empty marker', async () => {
  const owner = {}
  const deferred = beginSendRefresh(owner, 'dore')
  assert.equal(deferMessageRefresh(owner, 'dore'), true)
  let refreshes = 0
  await settleSendRefresh(deferred, false, async () => { refreshes++ })
  assert.equal(refreshes, 1)

  const quiet = beginSendRefresh(owner, 'dore')
  await settleSendRefresh(quiet, false, async () => { refreshes++ })
  assert.equal(refreshes, 1)
})

test('send refresh markers are scoped by query client and agent', async () => {
  const first = {}
  const second = {}
  const token = beginSendRefresh(first, 'dore')
  assert.equal(deferMessageRefresh(first, 'other'), false)
  assert.equal(deferMessageRefresh(second, 'dore'), false)
  assert.equal(deferMessageRefresh(first, 'dore'), true)
  await settleSendRefresh(token, true, () => undefined)
})
