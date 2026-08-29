import assert from 'node:assert/strict'
import test from 'node:test'

import { dockOpenTarget, openInSideLabel, placementFromModifiers } from '../src/features/layout/openPlacement.ts'

test('the shared modifier helper maps default and Mac Option events to one placement contract', () => {
  assert.deepEqual(placementFromModifiers({ altKey: false }, 'group-source'), { direction: 'within', groupID: 'group-source' })
  assert.deepEqual(placementFromModifiers({ altKey: true }, 'group-source'), { direction: 'right', groupID: 'group-source' })
  assert.equal(openInSideLabel('Mozilla/5.0 (Macintosh; Intel Mac OS X)'), '⌥+click: open in side split')
  assert.equal(openInSideLabel('Mozilla/5.0 (X11; Linux x86_64)'), 'Alt+click: open in side split')
})

test('a Mac-shaped Option click drives the shared dock path to the right of the active group', () => {
  const placement = placementFromModifiers({ altKey: true }, 'group-source')
  assert.deepEqual(dockOpenTarget(undefined, placement, { activeGroupID: 'group-active', firstGroupID: 'group-first' }), {
    kind: 'new',
    groupID: undefined,
    position: { referenceGroup: 'group-active', direction: 'right' },
  })
})

test('an already-open agent focuses by identity instead of creating an Option split', () => {
  const existing = { id: 'agent:impl-kima' }
  assert.deepEqual(dockOpenTarget(existing, placementFromModifiers({ altKey: true }), {
    activeGroupID: 'group-active', firstGroupID: 'group-first',
  }), { kind: 'existing', panel: existing })
})

test('default placement stays within the requested source group', () => {
  assert.deepEqual(dockOpenTarget(undefined, placementFromModifiers({ altKey: false }, 'group-source'), {
    activeGroupID: 'group-active', firstGroupID: 'group-first',
  }), {
    kind: 'new',
    groupID: 'group-source',
    position: { referenceGroup: 'group-source', direction: 'within' },
  })
})
