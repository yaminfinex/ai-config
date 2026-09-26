import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

import { statusChipChars, statusChipTruncates } from '../src/features/transcript/cleanView.ts'
import { deliveryExpandLabel, deliveryPreview, deliveryPreviewLimits, hcomDeliveryPresentation } from '../src/features/transcript/deliveryModel.ts'
import { Markdown } from '../src/shared/Markdown.ts'

// Real deliveries from conductor-line's transcript (2026-09-25/26), verbatim.
type Delivery = { sender: string, recipient: string, intent: string, message_id: string, thread?: string, text: string }
const fixtures = JSON.parse(readFileSync(new URL('./fixtures/hcomDeliveries.json', import.meta.url), 'utf8')) as Delivery[]
const byId = (id: string) => fixtures.find((delivery) => delivery.message_id === id)!
const transcriptEntriesSource = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')
const stylesSource = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')

const ownerWords = `sure can spawn to fix those
Another very annoying thing is that my messages when rendered, because they come via hcom, get collapsed into a string instead of proper formatting and it's a little frustrating
another somewhat frustrating thing is that _non human_ messages (ie from other agents) that land in the session are also _massive_ but they get dumped as one large thing instead of having a trimmed view + a way to expand, that feels like a miss
finally, the status pulls when rendered are truncated but there's no way to expand them and that sucks`

test('an operator delivery drops the note wrapper, keeps every line, and is never trimmed', () => {
  const message = hcomDeliveryPresentation(byId('320997'))
  assert.equal(message.operator, true)
  assert.equal(message.sender, 'web-yamen-core-infinex-gg')
  assert.equal(message.body, ownerWords)
  assert.equal(message.preview, null)
  assert.doesNotMatch(message.body, /HERDER_WEB_OPERATOR_NOTE|web operator named/)
})

test('operator text renders as markdown with its single line breaks kept', () => {
  const message = hcomDeliveryPresentation(byId('321474'))
  const html = renderToStaticMarkup(createElement(Markdown, { lineBreaks: true }, message.body))
  assert.equal(html, "<p>Yep let&#x27;s do it<br/>\nand can run transcript rendering in parallel<br/>\nall should be separate agents anyway</p>")
  const owner = renderToStaticMarkup(createElement(Markdown, { lineBreaks: true }, ownerWords))
  assert.equal(owner.match(/<br\/>/g)?.length, 3)
  assert.match(owner, /<em>non human<\/em>/)
  assert.match(owner, /<em>massive<\/em>/)
})

test('line breaks stay off by default and never enter code', () => {
  assert.doesNotMatch(renderToStaticMarkup(createElement(Markdown, null, 'one\ntwo')), /<br/)
  const html = renderToStaticMarkup(createElement(Markdown, { lineBreaks: true }, 'a `x`\n\n```\nline 1\nline 2\n```'))
  assert.match(html, /<code>line 1\nline 2\n<\/code>/)
  assert.doesNotMatch(html, /line 1<br/)
})

test('a long agent report opens as a short preview with sender, thread, and intent', () => {
  const message = hcomDeliveryPresentation(byId('321130'))
  assert.equal(message.operator, false)
  assert.equal(message.sender, 'impl-lino')
  assert.equal(message.thread, 'audit-fixes')
  assert.equal(message.intent, 'inform')
  assert.ok(message.preview)
  assert.equal(message.preview.text, 'audit-fixes DONE: 239c601f on branch audit-fixes (one commit, 11 files).\n\nFiles: .agents/skills/ai-config-bootstrap/SKILL.md, bin/ai-setup, docs/session-context-fleet.md, lib/common.sh, skills/design-an-interface/SKILL.md, skills/improve-architecture/SKILL.md, skills/show-me/{SKILL,VENDOR}.md, skills/wayfinder/{SKILL,VENDOR}.md, new tools/herder/tests/check-ai-setup-missing-source.sh\n\nItems:')
  assert.ok(message.body.startsWith(message.preview.text))
  assert.equal(message.preview.hiddenLines, message.body.split('\n').slice(5).filter((line) => line.trim()).length)
  assert.equal(deliveryExpandLabel(message.preview, false), `Show full message · ${message.preview.hiddenLines} more lines`)
  assert.equal(deliveryExpandLabel(message.preview, true), 'Show less')
})

test('every long fixture preview stays inside the limits; short messages get no control', () => {
  for (const delivery of fixtures) {
    const message = hcomDeliveryPresentation(delivery)
    const lines = message.body.split('\n')
    const long = lines.length > deliveryPreviewLimits.fullLines || message.body.length > deliveryPreviewLimits.fullChars
    if (message.operator || !long) {
      assert.equal(message.preview, null, delivery.message_id)
      continue
    }
    assert.ok(message.preview, delivery.message_id)
    assert.ok(message.preview.text.split('\n').length <= deliveryPreviewLimits.previewLines, delivery.message_id)
    assert.ok(message.preview.text.length <= deliveryPreviewLimits.previewChars + 1, delivery.message_id)
  }
  assert.equal(hcomDeliveryPresentation(byId('321059')).preview, null)
})

test('the preview cuts one huge line at a word and closes an open code fence', () => {
  const word = 'lorem '.repeat(200)
  const cut = deliveryPreview(word)
  assert.ok(cut)
  assert.ok(cut.text.endsWith('…'))
  assert.ok(cut.text.length <= deliveryPreviewLimits.previewChars + 1)
  assert.equal(cut.hiddenLines, 0)
  assert.equal(deliveryExpandLabel(cut, false), 'Show full message')

  const fenced = deliveryPreview(['Result:', '```', 'ok 1', 'ok 2', 'ok 3', 'ok 4', 'ok 5', 'ok 6', '```'].join('\n'))
  assert.ok(fenced)
  assert.equal(fenced.text, 'Result:\n```\nok 1\nok 2\nok 3\n```')
  assert.equal(fenced.hiddenLines, 4)
})

test('hcom cards render every delivery through the presentation and a real disclosure', () => {
  const cards = transcriptEntriesSource.slice(transcriptEntriesSource.indexOf('function HcomCards'), transcriptEntriesSource.indexOf('function ActivityStrip'))
  const body = transcriptEntriesSource.slice(transcriptEntriesSource.indexOf('function HcomMessageBody'), transcriptEntriesSource.indexOf('function formatDuration'))
  assert.match(cards, /hcomDeliveryPresentation\(objectValue\(raw\)\)/)
  assert.match(cards, /<HcomMessageBody body=\{message\.body\} preview=\{message\.preview\} \/>/)
  assert.doesNotMatch(cards, /<MentionText>/)
  assert.match(body, /<MentionMarkdown lineBreaks>/)
  assert.match(body, /<button type="button" className="hcom-expand" aria-expanded=\{expanded\} aria-controls=\{id\}/)
  assert.match(stylesSource, /\.hcom-body\.is-trimmed \{ mask-image: linear-gradient/)
})

test('status chips toggle only when their text passes the CSS cap', () => {
  assert.equal(statusChipChars, 26)
  assert.equal(statusChipTruncates('a'.repeat(26)), false)
  assert.equal(statusChipTruncates('a'.repeat(27)), true)
  assert.equal(statusChipTruncates('lino is fixing the --idle wording'), true)
  assert.equal(statusChipTruncates('lipe is reviewing audit-fixes'), true)
  assert.equal(statusChipTruncates('go on both; writing briefs'), false)
  assert.match(stylesSource, new RegExp(`\\.activity-pill\\.assistant-status \\{ max-width: calc\\(${statusChipChars}ch \\+ 16px\\);`))
  assert.match(stylesSource, /\.activity-pill \{ flex: 0 0 auto; padding: 1px 7px; border: 1px solid/)
  assert.match(stylesSource, /\.activity-pill\.status-chip-open \{ max-width: 100%;[^}]*white-space: pre-wrap/)
})

test('strip and inline status chips share one accessible disclosure', () => {
  const chip = transcriptEntriesSource.slice(transcriptEntriesSource.indexOf('function StatusChip'), transcriptEntriesSource.indexOf('function HcomMessageBody'))
  assert.match(chip, /if \(!statusChipTruncates\(text\)\) return <span className="activity-pill assistant-status">/)
  assert.match(chip, /<button type="button" className="activity-pill assistant-status status-chip" aria-expanded="false"/)
  assert.match(chip, /<button type="button" className="status-chip-toggle" aria-expanded="true" aria-label="Collapse status"/)
  assert.match(chip, /event\.preventDefault\(\)\s+event\.stopPropagation\(\)/)
  assert.match(transcriptEntriesSource, /pill\.tone === 'assistant-status'\s+\? <StatusChip/)
  assert.match(transcriptEntriesSource, /return <StatusChip text=\{segment\.content\} key=\{index\}><MentionText>/)
})
