import assert from 'node:assert/strict'
import test from 'node:test'
import type { Route } from '../src/shared/navigation.tsx'
import { layoutRouteState, shouldReplayInitialRoute } from '../src/features/layout/routeReplay.ts'

const agentRoute: Route = { page: 'agent', name: 'mavu' }

test('a restored layout wins over a panel-reflected route on refresh', () => {
  assert.equal(shouldReplayInitialRoute(agentRoute, layoutRouteState, true), false)
})

test('cold and deliberate deep links still replay their route', () => {
  assert.equal(shouldReplayInitialRoute(agentRoute, null, true), true)
  assert.equal(shouldReplayInitialRoute(agentRoute, {}, true), true)
  assert.equal(shouldReplayInitialRoute(agentRoute, layoutRouteState, false), true)
  assert.equal(shouldReplayInitialRoute({ page: 'shell' }, layoutRouteState, true), false)
})
