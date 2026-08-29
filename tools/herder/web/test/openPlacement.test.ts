import assert from 'node:assert/strict'
import test from 'node:test'

import { dockOpenTarget, openInSideLabel, placementFromModifiers } from '../src/features/layout/openPlacement.ts'

test('the shared modifier helper maps default and Mac Option events to one placement contract', () => {
  assert.deepEqual(placementFromModifiers({ altKey: false }, 'group-source'), { direction: 'within', groupID: 'group-source' })
  assert.deepEqual(placementFromModifiers({ altKey: true }, 'group-source'), { direction: 'right', groupID: 'group-source' })
  assert.equal(openInSideLabel('Mozilla/5.0 (Macintosh; Intel Mac OS X)'), '⌥+click: open in closest side group')
  assert.equal(openInSideLabel('Mozilla/5.0 (X11; Linux x86_64)'), 'Alt+click: open in closest side group')
})

test('a Mac-shaped Option click creates a right split only for the sole group', () => {
  const placement = placementFromModifiers({ altKey: true }, 'group-source')
  assert.deepEqual(dockOpenTarget(undefined, placement, { activeGroupID: 'group-active', firstGroupID: 'group-active', groupCount: 1 }), {
    kind: 'new',
    groupID: undefined,
    position: { referenceGroup: 'group-active', direction: 'right' },
  })
})

test('repeated Option opens reuse the right sibling, then the left sibling when no right exists', () => {
  const placement = placementFromModifiers({ altKey: true }, 'group-active')
  const right = { activeGroupID: 'group-active', firstGroupID: 'group-left', groupCount: 3, rightGroupID: 'group-right', leftGroupID: 'group-left' }
  const reusedRight = { kind: 'new', groupID: 'group-right', position: { referenceGroup: 'group-right', direction: 'within' } }
  assert.deepEqual(dockOpenTarget(undefined, placement, right), reusedRight)
  assert.deepEqual(dockOpenTarget(undefined, placement, right), reusedRight)
  assert.deepEqual(dockOpenTarget(undefined, placement, {
    activeGroupID: 'group-right', firstGroupID: 'group-left', groupCount: 3, leftGroupID: 'group-active',
  }), { kind: 'new', groupID: 'group-active', position: { referenceGroup: 'group-active', direction: 'within' } })
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
