import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { failureBanner } from '../src/shared/panelState.ts'

test('panel failures share one refusal-to-banner presentation', () => {
  const failure = failureBanner('git status', new Error('offline'))
  assert.equal(failure.source, 'git status')
  assert.equal(failure.detail, 'request failed: offline')
  assert.deepEqual(failure.problem, { error: 'request failed', detail: 'offline' })
})

test('file and changes panels share activation and state presentation', () => {
  const filePanel = readFileSync(new URL('../src/features/files/FilePanel.tsx', import.meta.url), 'utf8')
  const changesPanel = readFileSync(new URL('../src/features/git/ChangesPanel.tsx', import.meta.url), 'utf8')
  for (const source of [filePanel, changesPanel]) {
    assert.match(source, /useActivationRefetch\(/)
    assert.match(source, /<PanelState/)
  }
  assert.doesNotMatch(filePanel, /const wasActive = useRef/)
  assert.doesNotMatch(changesPanel, /const wasActive = useRef/)
})
