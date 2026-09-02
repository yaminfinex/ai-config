import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import type { TranscriptEntry } from '../src/types.ts'
import {
  aggregateActivityPills,
  approximateActivityAge,
  activityPillTone,
  cleanViewDisposition,
  isCleanConversationDelivery,
  markerOnlyAssistantActivity,
  persistTranscriptViewMode,
  readTranscriptViewMode,
  splitFinalActivityRun,
  transcriptViewPreferenceKey,
} from '../src/features/transcript/cleanView.ts'
import { cleanRows } from '../src/features/transcript/cleanRows.ts'

const transcriptEntriesSource = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')
const stylesSource = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')

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
    { key: 'read-1', label: 'Read', kind: 'tool_use', aggregation: { category: 'tool', content: 'Read' } },
    { key: 'read-2', label: 'Read', kind: 'tool_use', aggregation: { category: 'tool', content: 'Read' } },
    { key: 'message-1', label: '✉ nero', kind: 'message' },
    { key: 'message-2', label: '✉ nero', kind: 'message' },
    { key: 'read-3', label: 'Read', kind: 'tool_use', aggregation: { category: 'tool', content: 'Read' } },
  ]), [
    { key: 'read-1', label: 'Read', kind: 'tool_use', aggregation: { category: 'tool', content: 'Read' }, count: 2 },
    { key: 'message-1', label: '✉ nero', kind: 'message', count: 1 },
    { key: 'message-2', label: '✉ nero', kind: 'message', count: 1 },
    { key: 'read-3', label: 'Read', kind: 'tool_use', aggregation: { category: 'tool', content: 'Read' }, count: 1 },
  ])
})

test('marker-only assistant activity uses compact labels, proven tones, and exact repeat keys', () => {
  assert.deepEqual(markerOnlyAssistantActivity('<status>sent to ziru</status>'), {
    label: 'sent to ziru',
    tone: 'assistant-status',
    title: 'sent to ziru',
    aggregation: { category: 'assistant-status', content: '<status>sent to ziru</status>' },
  })
  assert.deepEqual(markerOnlyAssistantActivity('<internal>private detail</internal>'), {
    label: 'internal note',
    tone: 'thinking',
    title: 'private detail',
    aggregation: { category: 'assistant-internal', content: '<internal>private detail</internal>' },
  })
  assert.deepEqual(markerOnlyAssistantActivity('<status>sent</status><internal>private detail</internal>'), {
    label: 'sent · internal note',
    tone: 'assistant-status',
    title: 'sent\nprivate detail',
    aggregation: { category: 'assistant-mixed', content: '<status>sent</status><internal>private detail</internal>' },
  })
})

test('visible and phantom-open fail-open assistant text never becomes compact activity', () => {
  assert.equal(markerOnlyAssistantActivity('visible <status>sent</status>'), null)
  assert.equal(markerOnlyAssistantActivity('<internal>phantom-open poisoned message'), null)
})

test('marker pill counts require consecutive exact content and category matches', () => {
  const activities = [
    { key: 'internal-1', label: 'internal note', aggregation: { category: 'assistant-internal', content: '<internal>first</internal>' } },
    { key: 'internal-2', label: 'internal note', aggregation: { category: 'assistant-internal', content: '<internal>second</internal>' } },
    { key: 'internal-3', label: 'internal note', aggregation: { category: 'assistant-internal', content: '<internal>second</internal>' } },
    { key: 'status-1', label: 'internal note', aggregation: { category: 'assistant-status', content: '<status>internal note</status>' } },
    { key: 'tool-1', label: 'internal note', aggregation: { category: 'tool', content: 'internal note' } },
  ]
  assert.deepEqual(aggregateActivityPills(activities), [
    { ...activities[0], count: 1 },
    { ...activities[1], count: 2 },
    { ...activities[3], count: 1 },
    { ...activities[4], count: 1 },
  ])
})

test('compact row building joins marker-only entries and fail-open text splits runs', () => {
  const entry = (kind: TranscriptEntry['kind'], content: string, byteOffset: number): TranscriptEntry => ({
    line: byteOffset,
    byteOffset,
    kind,
    payload: kind === 'tool_use' ? { name: content } : { message: { content } },
  })
  const relationships = {
    pairedToolResults: new Set<number>(),
    pairedDeliveries: new Set<number>(),
    pairedCommandOutputs: new Set<number>(),
    duplicateHcomDeliveries: new Map<number, Set<number>>(),
  }
  const tool = entry('tool_use', 'Read', 1)
  const status = entry('assistant_text', '<status>sent</status>', 2)
  const phantom = entry('assistant_text', '<internal>phantom-open poisoned message', 3)
  const internal = entry('assistant_text', '<internal>private detail</internal>', 4)
  const visible = entry('assistant_text', 'visible <status>sent</status>', 5)
  const finalStatus = entry('assistant_text', '<status>done</status>', 6)

  const rows = cleanRows([tool, status, phantom, internal, visible, finalStatus], relationships)
  assert.deepEqual(rows.map((row) => row.type), ['run', 'entry', 'run', 'entry', 'run'])
  assert.deepEqual(rows[0].type === 'run' ? rows[0].activities.map((activity) => activity.entry.byteOffset) : [], [1, 2])
  assert.equal(rows[1].type === 'entry' ? rows[1].entry : null, phantom)
  assert.deepEqual(rows[2].type === 'run' ? rows[2].activities.map((activity) => activity.entry.byteOffset) : [], [4])
  assert.equal(rows[3].type === 'entry' ? rows[3].entry : null, visible)
  assert.equal(splitFinalActivityRun(rows)?.latest.entry, finalStatus)

  const superseded = cleanRows([...([tool, status] as TranscriptEntry[]), entry('human_prompt', 'owner reply', 7)], relationships)
  assert.equal(splitFinalActivityRun(superseded), null)
  assert.deepEqual(superseded[0].type === 'run' ? superseded[0].activities.map((activity) => activity.entry.byteOffset) : [], [1, 2])
})

test('compact rows keep always-visible model fallbacks and drop other system chips', () => {
  const entry = (kind: TranscriptEntry['kind'], payload: unknown, byteOffset: number): TranscriptEntry => ({
    line: byteOffset,
    byteOffset,
    kind,
    payload,
  })
  const assistant = entry('assistant_text', { message: { content: 'visible reply' } }, 1)
  const refusal = entry('system_chip', {
    type: 'system',
    subtype: 'model_refusal_fallback',
    fallbackModel: 'claude-opus-4-8',
    content: "Fable 5's safeguards flagged this message. Switched to Opus 4.8.",
  }, 2)
  const relocated = entry('system_chip', { type: 'relocated', relocatedCwd: '/invented/violet-worktree' }, 3)
  const scheduled = entry('system_chip', { type: 'system', subtype: 'scheduled_task_fire' }, 4)
  const relationships = {
    pairedToolResults: new Set<number>(),
    pairedDeliveries: new Set<number>(),
    pairedCommandOutputs: new Set<number>(),
    duplicateHcomDeliveries: new Map<number, Set<number>>(),
  }

  const rows = cleanRows([assistant, refusal, relocated, scheduled], relationships)
  assert.deepEqual(rows.map((row) => row.type === 'entry' ? row.entry.byteOffset : null), [1, 2])
  assert.equal(rows[1].type === 'entry' ? rows[1].entry : null, refusal)
})

test('compact strip wraps accessibly', () => {
  const strip = transcriptEntriesSource.slice(transcriptEntriesSource.indexOf('function ActivityStrip'), transcriptEntriesSource.indexOf('function ActivityEntry'))
  const summaryRule = stylesSource.match(/\.activity-strip > summary \{([^}]*)\}/)?.[1] ?? ''

  assert.match(strip, /<summary aria-label=/)
  assert.match(strip, /title=\{pill\.title\}/)
  assert.match(summaryRule, /flex-wrap:\s*wrap/)
  assert.doesNotMatch(summaryRule, /overflow-x:\s*auto/)
  assert.match(stylesSource, /\.activity-strip > summary::before \{[^}]*flex:\s*0 0 auto/s)
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

  assert.match(transcriptEntriesSource, /\{finalActivity\.collapsed\.length > 0 && <ActivityStrip/)
})

test('a trailing show entry including fenced assistant text prevents the final activity hoist', () => {
  assert.equal(splitFinalActivityRun([
    { type: 'run', activities: [{ key: 'read-1', label: 'Read' }] },
    { type: 'entry', kind: 'assistant_text' },
  ]), null)

  assert.match(transcriptEntriesSource, /if \(entry\.kind === 'assistant_text'\) return <AssistantText/)
  assert.match(transcriptEntriesSource, /if \(cleanView\) \{[\s\S]+splitFinalActivityRun\(rows\)[\s\S]+return <MentionContext\.Provider/)
  assert.match(transcriptEntriesSource, /finalActivity && rowIndex === rows\.length - 1/)
  assert.match(transcriptEntriesSource, /return <MentionContext\.Provider value=\{mentionContext\}>\{entries\.map\(\(entry, index\) => <EntryView/)
  assert.match(transcriptEntriesSource, /approximateActivityAge\(activity\.entry\.timestamp, now\)/)
  assert.doesNotMatch(transcriptEntriesSource, /setInterval/)
})

test('the live activity reuses the Normal entry renderer and resets only when superseded', () => {
  const latestActivity = transcriptEntriesSource.slice(transcriptEntriesSource.indexOf('function LatestActivity'), transcriptEntriesSource.indexOf('function AssistantText'))

  assert.match(transcriptEntriesSource, /function ActivityEntry\([\s\S]+<EntryView[\s\S]+<HcomCards/)
  assert.match(latestActivity, /<ActivityEntry/)
  assert.doesNotMatch(latestActivity, /activity-pill/)
  assert.match(transcriptEntriesSource, /<LatestActivity activity=\{finalActivity\.latest\}[\s\S]+key=\{finalActivity\.latest\.key\}/)
})

test('live detail shares the strip showSystem policy without changing Normal', () => {
  const entryView = transcriptEntriesSource.slice(transcriptEntriesSource.indexOf('function EntryView'), transcriptEntriesSource.indexOf('export function TranscriptEntries'))
  const transcriptEntries = transcriptEntriesSource.slice(transcriptEntriesSource.indexOf('export function TranscriptEntries'))
  const activityEntries = transcriptEntriesSource.match(/<ActivityEntry\b[^>]*\/>/g) ?? []

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
