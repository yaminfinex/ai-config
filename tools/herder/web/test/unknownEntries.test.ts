import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import type { TranscriptEntry } from '../src/types.ts'
import { messageText } from '../src/features/transcript/cleanRows.ts'

const stylesheet = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
const transcriptEntriesSource = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')

function luminance(hex: string) {
  const channels = [1, 3, 5].map((index) => Number.parseInt(hex.slice(index, index + 2), 16) / 255)
    .map((value) => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
}

function contrast(foreground: string, background: string) {
  const values = [luminance(foreground), luminance(background)].sort((a, b) => b - a)
  return (values[0] + 0.05) / (values[1] + 0.05)
}

test('relocated entries present one plain system line behind Show System', async () => {
  const { systemEntryPresentation } = await import('../src/features/transcript/systemEntries.ts')
  assert.deepEqual(systemEntryPresentation({ type: 'relocated', relocatedCwd: '/invented/violet-worktree' }), {
    subtype: 'relocated',
    summary: 'session moved to /invented/violet-worktree',
    detail: '',
    alwaysVisible: false,
  })
})

test('model refusal fallbacks lead with the switch and retain CLI content as detail', async () => {
  const { systemEntryPresentation } = await import('../src/features/transcript/systemEntries.ts')
  const content = "Fable 5's safeguards flagged this message. Switched to Opus 4.8."
  assert.deepEqual(systemEntryPresentation({
    type: 'system',
    subtype: 'model_refusal_fallback',
    fallbackModel: 'claude-opus-4-8',
    content,
  }), {
    subtype: 'model_refusal_fallback',
    summary: 'model switched to Opus 4.8 — safeguards flagged a message',
    detail: content,
    alwaysVisible: true,
  })
})

test('model consent fallbacks lead with the switch and retain CLI content as detail', async () => {
  const { systemEntryPresentation } = await import('../src/features/transcript/systemEntries.ts')
  const content = 'Switched to Opus 4.8 (1M context) for this session · Fable 5 requires usage credits · /model to change'
  assert.deepEqual(systemEntryPresentation({
    type: 'system',
    subtype: 'model_consent_fallback',
    fallbackModel: 'claude-opus-4-8[1m]',
    content,
  }), {
    subtype: 'model_consent_fallback',
    summary: 'model switched to Opus 4.8 — consent required',
    detail: content,
    alwaysVisible: true,
  })
})

test('unknown entry labels retain the original type and system subtype', async () => {
  const { unknownEntryLabel } = await import('../src/features/transcript/systemEntries.ts')
  const entry = (payload: unknown): TranscriptEntry => ({ line: 1, byteOffset: 0, kind: 'unknown', payload })
  assert.equal(unknownEntryLabel(entry({ type: 'future-sidecar' })), 'unknown entry · future-sidecar')
  assert.equal(unknownEntryLabel(entry({ type: 'system', subtype: 'future-event' })), 'unknown entry · system/future-event')
})

test('system local command content reaches the existing command renderer unwrap', () => {
  assert.equal(messageText({ type: 'system', subtype: 'local_command', content: '<local-command-stdout>Reloaded fixture skills</local-command-stdout>' }), '<local-command-stdout>Reloaded fixture skills</local-command-stdout>')
})

test('message content remains authoritative when top-level content is also present', () => {
  assert.equal(messageText({
    content: 'top-level system content',
    message: { content: 'nested message content' },
  }), 'nested message content')
})

test('model switch styling is informational and WCAG AA in both themes', () => {
  assert.match(transcriptEntriesSource, /systemEntry\?\.alwaysVisible[^\n]*role="status"/)
  assert.match(stylesheet, /\.model-switch-entry \{[^}]*border-color: var\(--info-border\);[^}]*background: var\(--info-bg\);[^}]*color: var\(--info-text\)/s)
  assert.doesNotMatch(stylesheet.match(/\.model-switch-entry \{[^}]*\}/s)?.[0] ?? '', /(?:red|error)/)
  for (const block of stylesheet.matchAll(/:root\[data-theme='(?:light|dark)'\] \{([\s\S]*?)\n\}/g)) {
    const foreground = block[1].match(/--info-text: (#[\da-f]{6})/i)?.[1]
    const background = block[1].match(/--info-bg: (#[\da-f]{6})/i)?.[1]
    assert.ok(foreground && background)
    assert.ok(contrast(foreground, background) >= 4.5)
  }
})
