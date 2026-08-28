import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { bannerSemantics } from '../src/shared/bannerPresentation.ts'
import { screenNotice, transcriptNotice } from '../src/shared/loadingPresentation.ts'

test('shared banners distinguish quiet information from dismissible errors', () => {
  assert.deepEqual(bannerSemantics('info'), { className: 'banner info', role: 'status', dismissible: false })
  assert.deepEqual(bannerSemantics('error'), { className: 'banner error', role: 'alert', dismissible: true })
})

test('a pending transcript query is neutral and errors appear only after failure', () => {
  assert.deepEqual(transcriptNotice(true, ''), { tone: 'info', detail: 'Loading transcript…' })
  assert.equal(transcriptNotice(false, ''), null)
  assert.deepEqual(transcriptNotice(false, 'read failed'), { tone: 'error', detail: 'read failed' })
})

test('screen first-load and truncation are informational while unavailable is an error', () => {
  assert.deepEqual(screenNotice(undefined), { tone: 'info', detail: 'Connecting to live pane…' })
  assert.equal(screenNotice({ pane_id: 'w1:p2', revision: 1, status: 'available', text: 'ready', truncated: false }), null)
  assert.deepEqual(screenNotice({ pane_id: 'w1:p2', revision: 1, status: 'available', text: 'partial', truncated: true }), {
    tone: 'info', detail: 'Screen exceeds the 16 KiB live-frame budget; this snapshot is truncated.',
  })
  assert.deepEqual(screenNotice({ pane_id: 'w1:p2', status: 'unavailable', text: '', truncated: false, detail: 'pane gone' }), {
    tone: 'error', detail: 'pane gone',
  })
})

test('screen loading notice overlays a header and terminal that remain mounted', () => {
  const component = readFileSync(new URL('../src/features/screen/ScreenPanel.tsx', import.meta.url), 'utf8')
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  assert.match(component, /className="screen-viewport" aria-busy=\{!frame\}/)
  assert.match(component, /className="screen-notice"/)
  assert.match(component, /className=\{`terminal-screen/)
  assert.match(css, /\.screen-notice \{[^}]*position: absolute;/s)
})
