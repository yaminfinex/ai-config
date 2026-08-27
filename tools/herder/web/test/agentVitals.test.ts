import assert from 'node:assert/strict'
import test from 'node:test'

import { agentVitalsPresentation } from '../src/shared/agentVitals.ts'

test('agent vitals render only facts supplied by real session-shaped fixtures', () => {
  assert.deepEqual(agentVitalsPresentation({
    model: 'invented-codex-model',
    context_usage: { used_tokens: 112600, input_tokens: 112600, cached_input_tokens: 101120, output_tokens: 266, window_tokens: 258400, used_percent: 43.57585139318885 },
  }), ['invented-codex-model', '113k tokens · 56% left'])

  assert.deepEqual(agentVitalsPresentation({
    model: 'invented-claude-model',
    context_usage: { used_tokens: 155069, input_tokens: 2, cache_creation_input_tokens: 686, cache_read_input_tokens: 154381, output_tokens: 682 },
  }), ['invented-claude-model', '155k tokens'])

  assert.deepEqual(agentVitalsPresentation({}), [])
})
