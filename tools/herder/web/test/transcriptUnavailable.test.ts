import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { transcriptUnavailable } from '../src/features/transcript/transcriptUnavailable.ts'

function refusal(error: string, detail: string) {
  return Object.assign(new Error(detail), {
    response: new Response(null, { status: 409 }),
    problem: { error, detail },
  })
}

test('missing independent subagent transcript is a calm parent-aware state', () => {
  assert.deepEqual(transcriptUnavailable(refusal('no independent transcript', 'subagent transcript unavailable'), 'probe-fame'), {
    title: 'No independent transcript for this subagent',
    detail: 'This subagent has no independent transcript. Open its parent, probe-fame.',
    parent: 'probe-fame',
  })
  assert.deepEqual(transcriptUnavailable(refusal('no independent transcript', 'subagent transcript unavailable'), undefined), {
    title: 'No independent transcript for this subagent',
    detail: 'This subagent has no independent transcript of its own.',
  })
  assert.equal(transcriptUnavailable(refusal('no session', 'ordinary failure'), 'probe-fame'), null)
})

test('agent panel renders the refusal as status and opens the known parent', () => {
  const source = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  assert.match(source, /transcriptUnavailable\(entriesQuery\.error, agent\?\.parent_agent\)/)
  assert.match(source, /transcriptNotice\(entriesQuery\.isPending, unavailable \? '' :/)
  assert.match(source, /<PanelState[^>]*className="transcript-unavailable"/)
  assert.match(source, /onOpenAgent\(unavailableParent\)/)
})
