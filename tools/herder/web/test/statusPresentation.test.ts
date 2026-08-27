import assert from 'node:assert/strict'
import test from 'node:test'

import { agentStatusPresentation } from '../src/shared/agentStatus.ts'

test('fleet bus statuses map to honest operator-facing lifecycle semantics', () => {
  assert.deepEqual(agentStatusPresentation('active'), {
    className: 'active', label: 'active', meaning: 'agent is currently working',
  })
  assert.deepEqual(agentStatusPresentation('listening'), {
    className: 'listening', label: 'listening', meaning: 'agent is available and waiting',
  })
  assert.deepEqual(agentStatusPresentation('blocked'), {
    className: 'blocked', label: 'blocked', meaning: 'agent cannot proceed',
  })
  assert.deepEqual(agentStatusPresentation('-'), {
    className: 'unknown', label: 'unknown', meaning: 'agent status is unavailable',
  })
  assert.deepEqual(agentStatusPresentation('invented-future-state'), {
    className: 'unknown', label: 'invented-future-state', meaning: 'agent status is unavailable',
  })
})
