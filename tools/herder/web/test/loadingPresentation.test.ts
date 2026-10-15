import assert from 'node:assert/strict'
import test from 'node:test'

import { bannerSemantics } from '../src/shared/bannerPresentation.ts'
import { transcriptNotice } from '../src/shared/loadingPresentation.ts'

test('shared banners distinguish quiet information from dismissible errors', () => {
  assert.deepEqual(bannerSemantics('info'), { className: 'banner info', role: 'status', dismissible: false })
  assert.deepEqual(bannerSemantics('error'), { className: 'banner error', role: 'alert', dismissible: true })
})

test('a pending transcript query is neutral and errors appear only after failure', () => {
  assert.deepEqual(transcriptNotice(true, ''), { tone: 'info', detail: 'Loading transcript…' })
  assert.equal(transcriptNotice(false, ''), null)
  assert.deepEqual(transcriptNotice(false, 'read failed'), { tone: 'error', detail: 'read failed' })
})

