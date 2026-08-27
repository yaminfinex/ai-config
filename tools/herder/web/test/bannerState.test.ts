import assert from 'node:assert/strict'
import test from 'node:test'

import { bannerState } from '../src/shared/bannerState.ts'

test('an error banner stays dismissed until its source or detail changes', () => {
  const visible = { key: 'transcript:retired\u0000not found', dismissed: false }
  const dismissed = bannerState(visible, { type: 'dismiss' })
  assert.deepEqual(dismissed, { ...visible, dismissed: true })
  assert.equal(bannerState(dismissed, { type: 'sync', key: visible.key }), dismissed)
  assert.deepEqual(bannerState(dismissed, { type: 'sync', key: 'transcript:retired\u0000new failure' }), {
    key: 'transcript:retired\u0000new failure', dismissed: false,
  })
})
