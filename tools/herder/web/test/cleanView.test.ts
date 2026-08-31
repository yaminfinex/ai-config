import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  aggregateActivityPills,
  approximateActivityAge,
  activityPillTone,
  cleanViewDisposition,
  isCleanConversationDelivery,
  persistTranscriptViewMode,
  readTranscriptViewMode,
  splitFinalActivityRun,
  transcriptViewPreferenceKey,
} from '../src/features/transcript/cleanView.ts'

function memoryStorage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
  }
}

test('fixture law classifies every transcript kind explicitly', () => {
  assert.deepEqual(cleanViewDisposition, {
    human_prompt: 'show',
    hcom_delivery_stub: 'delivery',
    hcom_delivery: 'delivery',
    task_notification: 'activity',
    injected_system: 'system',
    command_stdout: 'activity',
    compact_divider: 'show',
    assistant_text: 'show',
    thinking: 'activity',
    tool_use: 'activity',
    tool_result: 'activity',
    turn_duration: 'hide',
    system_chip: 'system',
    unknown: 'activity',
  })
})

test('clean conversation delivery policy hides lifecycle traffic without guessing from text', () => {
  assert.equal(isCleanConversationDelivery({ sender: '[hcom-launcher]', intent: 'new message', text: 'agent ready' }), false)
  assert.equal(isCleanConversationDelivery({ sender: 'impl-vile', intent: 'ack', text: 'acknowledged' }), false)
  assert.equal(isCleanConversationDelivery({ sender: 'impl-vile', intent: 'request', text: 'please inspect this' }), true)
  assert.equal(isCleanConversationDelivery({ sender: 'impl-vile', intent: 'inform', text: 'work is done' }), true)
  assert.equal(isCleanConversationDelivery({ sender: 'web-owner', intent: 'new message', text: 'operator question' }), true)
  assert.equal(isCleanConversationDelivery({ sender: 'impl-vile', intent: 'new message', text: 'ordinary delivery' }), true)
})

test('transcript view mode is persisted per agent and defaults to compact', () => {
  const storage = memoryStorage()
  assert.equal(readTranscriptViewMode('agent one', storage), 'compact')
  persistTranscriptViewMode('agent one', 'normal', storage)
  persistTranscriptViewMode('agent two', 'full', storage)
  assert.equal(readTranscriptViewMode('agent one', storage), 'normal')
  assert.equal(readTranscriptViewMode('agent two', storage), 'full')
  assert.notEqual(transcriptViewPreferenceKey('agent one'), transcriptViewPreferenceKey('agent two'))
})

test('legacy true preferences migrate once to their equivalent transcript mode', () => {
  const storage = memoryStorage()
  storage.setItem('herder.web.showSystem.v1:full-agent', 'true')
  storage.setItem('herder.web.cleanView.v1:compact-agent', 'true')
  storage.setItem('herder.web.cleanView.v1:both-agent', 'true')
  storage.setItem('herder.web.showSystem.v1:both-agent', 'true')

  assert.equal(readTranscriptViewMode('full-agent', storage), 'full')
  assert.equal(readTranscriptViewMode('compact-agent', storage), 'compact')
  assert.equal(readTranscriptViewMode('both-agent', storage), 'compact')

  storage.removeItem('herder.web.showSystem.v1:full-agent')
  assert.equal(readTranscriptViewMode('full-agent', storage), 'full', 'the new key makes migration one-time')
})

test('blocked browser storage degrades to compact without throwing', () => {
  const blocked = {
    getItem: () => { throw new Error('blocked') },
    setItem: () => { throw new Error('blocked') },
    removeItem: () => { throw new Error('blocked') },
  }
  assert.equal(readTranscriptViewMode('agent', blocked), 'compact')
  assert.doesNotThrow(() => persistTranscriptViewMode('agent', 'full', blocked))
})

test('agent header exposes one three-way segmented transcript control', () => {
  const component = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(component, /Checkbox|clean view|show system entries/)
  assert.match(component, />Compact</)
  assert.match(component, />Normal</)
  assert.match(component, />Full</)
})

test('compact activity pills use distinct non-error semantic tones', () => {
  assert.equal(activityPillTone('tool_use'), 'tool')
  assert.equal(activityPillTone('tool_result'), 'tool')
  assert.equal(activityPillTone('command_stdout'), 'tool')
  assert.equal(activityPillTone('thinking'), 'thinking')
  assert.equal(activityPillTone('hcom_delivery', true), 'message')
  assert.equal(activityPillTone('task_notification'), 'other')
  assert.equal(activityPillTone('unknown'), 'other')
})

test('pill aggregation combines only consecutive repeatable activities', () => {
  assert.deepEqual(aggregateActivityPills([
    { key: 'read-1', label: 'Read', kind: 'tool_use' },
    { key: 'read-2', label: 'Read', kind: 'tool_use' },
    { key: 'message-1', label: '✉ nero', kind: 'message' },
    { key: 'message-2', label: '✉ nero', kind: 'message' },
    { key: 'read-3', label: 'Read', kind: 'tool_use' },
  ], (activity) => activity.kind === 'tool_use'), [
    { key: 'read-1', label: 'Read', kind: 'tool_use', count: 2 },
    { key: 'message-1', label: '✉ nero', kind: 'message', count: 1 },
    { key: 'message-2', label: '✉ nero', kind: 'message', count: 1 },
    { key: 'read-3', label: 'Read', kind: 'tool_use', count: 1 },
  ])
})

test('final activity split selects the raw latest item before pill aggregation', () => {
  const first = { key: 'read-1', label: 'Read', timestamp: '2026-08-31T10:00:00.000Z' }
  const latest = { key: 'read-2', label: 'Read', timestamp: '2026-08-31T10:04:00.000Z' }

  assert.deepEqual(splitFinalActivityRun([
    { type: 'entry' },
    { type: 'run', activities: [first, latest] },
  ]), { collapsed: [first], latest })
})

test('a one-item final activity run has no collapsed remainder', () => {
  const latest = { key: 'bash-1', label: 'Bash' }
  assert.deepEqual(splitFinalActivityRun([
    { type: 'entry' },
    { type: 'run', activities: [latest] },
  ]), { collapsed: [], latest })

  const component = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')
  assert.match(component, /\{finalActivity\.collapsed\.length > 0 && <ActivityStrip/)
})

test('a trailing show entry including fenced assistant text prevents the final activity hoist', () => {
  assert.equal(splitFinalActivityRun([
    { type: 'run', activities: [{ key: 'read-1', label: 'Read' }] },
    { type: 'entry', kind: 'assistant_text' },
  ]), null)

  const component = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')
  assert.match(component, /if \(entry\.kind === 'assistant_text'\) return <AssistantText/)
  assert.match(component, /if \(cleanView\) \{[\s\S]+splitFinalActivityRun\(rows\)[\s\S]+return <MentionContext\.Provider/)
  assert.match(component, /finalActivity && rowIndex === rows\.length - 1/)
  assert.match(component, /return <MentionContext\.Provider value=\{mentionContext\}>\{entries\.map\(\(entry, index\) => <EntryView/)
  assert.match(component, /approximateActivityAge\(activity\.entry\.timestamp, now\)/)
  assert.doesNotMatch(component, /setInterval/)
})

test('the live activity reuses the Normal entry renderer and resets only when superseded', () => {
  const component = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')
  const latestActivity = component.slice(component.indexOf('function LatestActivity'), component.indexOf('function AssistantText'))

  assert.match(component, /function ActivityEntry\([\s\S]+<EntryView[\s\S]+<HcomCards/)
  assert.match(latestActivity, /<ActivityEntry/)
  assert.doesNotMatch(latestActivity, /activity-pill/)
  assert.match(component, /<LatestActivity activity=\{finalActivity\.latest\}[\s\S]+key=\{finalActivity\.latest\.key\}/)
})

test('live detail shares the strip showSystem policy without changing Normal', () => {
  const component = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')
  const entryView = component.slice(component.indexOf('function EntryView'), component.indexOf('export function TranscriptEntries'))
  const transcriptEntries = component.slice(component.indexOf('export function TranscriptEntries'))
  const activityEntries = component.match(/<ActivityEntry\b[^>]*\/>/g) ?? []

  assert.equal(activityEntries.length, 2)
  assert.ok(activityEntries.every((activityEntry) => /\bshowSystem(?:\s|\/>)/.test(activityEntry)))
  assert.match(entryView, /if \(!showSystem\) return null/)
  assert.match(transcriptEntries, /return <MentionContext\.Provider value=\{mentionContext\}>\{entries\.map\([\s\S]+showSystem=\{showSystem\} cleanView=\{cleanView\}/)
})

test('compact activity age is terse and derived from the supplied clock', () => {
  const now = Date.parse('2026-08-31T10:04:00.000Z')
  assert.equal(approximateActivityAge('2026-08-31T10:00:00.000Z', now), '4m')
  assert.equal(approximateActivityAge(undefined, now), 'time unknown')
})
