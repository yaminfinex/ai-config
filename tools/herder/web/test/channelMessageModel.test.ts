import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { cleanRows } from '../src/features/transcript/cleanRows.ts'
import { channelInitials, channelMessagePresentation, channelSenderName } from '../src/features/transcript/channelMessageModel.ts'
import type { TranscriptEntry } from '../src/types.ts'

// The serve's channel_message payload for invented senders (see
// internal/claudesession/testdata/channel.jsonl).
const payload = {
  server: 'plugin:invented-transcript:inventa',
  source_label: 'inventa',
  messages: [
    { source: 'plugin:invented-transcript:inventa', source_label: 'inventa', message_id: 'session-input-one', created_at: '2026-01-02T03:04:05.000Z', session_id: 'invented-channel-session', sender: { kind: 'human', name: 'Invented Person', login: 'invented-login' }, text: 'hello **from** outside' },
    { source: 'plugin:invented-transcript:inventa', source_label: 'inventa', message_id: 'session-input-two', created_at: '', sender: { kind: 'human', name: '', login: 'second-login' }, text: 'second' },
    { source: 'plugin:invented-transcript:inventa', source_label: 'inventa', message_id: 'session-input-three', sender_raw: '{"kind":"human", broken', text: 'malformed sender' },
    { raw: true, text: 'text outside any channel tag' },
  ],
}

const transcriptEntriesSource = readFileSync(new URL('../src/features/transcript/TranscriptEntries.tsx', import.meta.url), 'utf8')

test('channel cards keep every block in order, raw text included', () => {
  const cards = channelMessagePresentation(payload)
  assert.deepEqual(cards.map((card) => card.kind), ['message', 'message', 'message', 'raw'])
  const [first, second, third, raw] = cards
  assert.equal(first.kind === 'message' && first.sender, 'Invented Person')
  assert.equal(first.kind === 'message' && first.initials, 'IP')
  assert.equal(first.kind === 'message' && first.sourceLabel, 'inventa')
  assert.equal(first.kind === 'message' && first.createdAt, '2026-01-02T03:04:05.000Z')
  assert.equal(first.kind === 'message' && first.body, 'hello **from** outside', 'the body stays markdown')
  assert.equal(first.kind === 'message' && first.preview, null, 'a short message shows in full')
  assert.equal(second.kind === 'message' && second.sender, 'second-login', 'login when there is no name')
  assert.equal(third.kind === 'message' && third.sender, '{"kind":"human", broken', 'a malformed sender degrades to its raw text')
  assert.deepEqual(raw, { kind: 'raw', key: 'raw:3', sourceLabel: 'inventa', body: 'text outside any channel tag' })
})

test('a long channel message opens as a preview like an hcom delivery', () => {
  const body = Array.from({ length: 12 }, (_, index) => `line ${index}`).join('\n')
  const [card] = channelMessagePresentation({ messages: [{ text: body, sender: { name: 'x' } }] })
  assert.ok(card.kind === 'message' && card.preview && card.preview.hiddenLines > 0)
})

test('sender names and initials degrade without throwing', () => {
  assert.equal(channelSenderName({}), 'unknown sender')
  assert.equal(channelSenderName({ sender: 'not an object' }), 'unknown sender')
  assert.equal(channelInitials('invented-login'), 'IL')
  assert.equal(channelInitials('solo'), 'SO')
  assert.equal(channelInitials('—'), '?')
  assert.deepEqual(channelMessagePresentation(null), [])
  assert.deepEqual(channelMessagePresentation({ messages: 'nope' }), [])
})

test('the card never loads a remote avatar and looks unlike prompts and deliveries', () => {
  const card = transcriptEntriesSource.slice(transcriptEntriesSource.indexOf('function ChannelCards'), transcriptEntriesSource.indexOf('function ActivityStrip'))
  assert.ok(card.length > 0)
  assert.doesNotMatch(card, /<img|avatarUrl|avatar_url/)
  assert.match(card, /channel-card/)
  assert.doesNotMatch(card, /human-entry|hcom-card/)
  assert.match(transcriptEntriesSource, /entry\.kind === 'channel_message'\) return <ChannelCards/)
})

test('compact view shows a channel message as conversation, not a pill', () => {
  const entry: TranscriptEntry = { kind: 'channel_message', byteOffset: 10, line: 1, timestamp: 't', payload }
  const rows = cleanRows([entry], { pairedToolResults: new Set(), pairedDeliveries: new Set(), pairedCommandOutputs: new Set(), duplicateHcomDeliveries: new Map() })
  assert.deepEqual(rows.map((row) => row.type), ['entry'])
})
