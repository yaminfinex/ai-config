import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { parseAssistantFencing } from '../src/features/transcript/fencingModel.ts'

test('plain assistant text stays a single literal text segment', () => {
  assert.deepEqual(parseAssistantFencing('ordinary **markdown**'), {
    fenced: false,
    hasVisibleText: true,
    segments: [{ kind: 'text', content: 'ordinary **markdown**' }],
  })
})

test('balanced sequential fences preserve outside text and count internal words', () => {
  assert.deepEqual(parseAssistantFencing('Before\n\n<internal>two words\nplus two</internal>\n<status>sent to ziru</status>\n\nAfter'), {
    fenced: true,
    hasVisibleText: true,
    segments: [
      { kind: 'text', content: 'Before\n\n' },
      { kind: 'internal', content: 'two words\nplus two', wordCount: 4 },
      { kind: 'text', content: '\n' },
      { kind: 'status', content: 'sent to ziru' },
      { kind: 'text', content: '\n\nAfter' },
    ],
  })
})

test('internal bodies may be multiline, empty, or repeated', () => {
  assert.deepEqual(parseAssistantFencing('<internal>\nfirst line\nsecond line\n</internal><internal></internal>'), {
    fenced: true,
    hasVisibleText: false,
    segments: [
      { kind: 'internal', content: '\nfirst line\nsecond line\n', wordCount: 4 },
      { kind: 'internal', content: '', wordCount: 0 },
    ],
  })
})

test('only non-whitespace outside text needs an assistant card shell', () => {
  assert.equal(parseAssistantFencing('<internal>only note</internal>').hasVisibleText, false)
  assert.equal(parseAssistantFencing('\n<status>sent</status>\n').hasVisibleText, false)
  assert.equal(parseAssistantFencing('Visible\n<internal>note</internal>').hasVisibleText, true)
})

test('exact lowercase tags leave other spellings literal', () => {
  const content = '<Internal>visible</Internal> <status data-kind="cheap">visible</status>'
  assert.deepEqual(parseAssistantFencing(content), {
    fenced: false,
    hasVisibleText: true,
    segments: [{ kind: 'text', content }],
  })
})

for (const [name, content] of [
  ['unclosed opening tag', 'before <internal>unfinished'],
  ['stray closing tag', 'before </status> after'],
  ['misordered closing tag', '<internal>body</status>'],
  ['nested same-kind tag', '<internal>outer <internal>inner</internal></internal>'],
  ['nested cross-kind tag', '<internal>outer <status>inner</status></internal>'],
  ['multiline status body', '<status>first\nsecond</status>'],
] as const) {
  test(`${name} makes the whole message fail open`, () => {
    assert.deepEqual(parseAssistantFencing(content), {
      fenced: false,
      hasVisibleText: true,
      segments: [{ kind: 'text', content }],
    })
  })
}

test('assistant rendering alone wires fences and Full forces internal details open', () => {
  const component = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')
  assert.equal(component.match(/<AssistantText/g)?.length, 1)
  assert.match(component, /if \(entry\.kind === 'assistant_text'\) return <AssistantText/)
  assert.match(component, /<details className="internal-note" open=\{showSystem\} onToggle=\{event => \{ if \(showSystem\) event\.currentTarget\.open = true \}\}[^>]*><summary className="activity-pill thinking">/)
  assert.match(component, /fencing\.hasVisibleText/)
  assert.match(component, /if \(entry\.kind === 'human_prompt'\) return <article className="entry-card human-entry"/)
})

test('internal pills and status chips use established AA theme token pairs', () => {
  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  const component = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')
  assert.match(component, /<summary className="activity-pill thinking">/)
  assert.match(component, /<span className="activity-pill assistant-status"/)
  assert.match(css, /\.assistant-status \{[^}]*var\(--info-border\)[^}]*var\(--info-bg\)[^}]*var\(--info-text\)/s)
})
