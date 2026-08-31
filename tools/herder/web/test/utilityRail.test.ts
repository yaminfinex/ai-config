import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  clampRailWidth,
  defaultRailPreferences,
  resizedRailWidth,
  railWidthFromKey,
} from '../src/features/layout/utilityRailModel.ts'

test('rail widths clamp and right-edge resizing mirrors the left edge', () => {
  assert.equal(clampRailWidth(120), 200)
  assert.equal(clampRailWidth(500), 440)
  assert.equal(resizedRailWidth(250, 'left', 20), 270)
  assert.equal(resizedRailWidth(250, 'right', 20), 230)
  assert.equal(railWidthFromKey(250, 'left', 'ArrowRight'), 260)
  assert.equal(railWidthFromKey(250, 'right', 'ArrowLeft'), 260)
})

test('new rail preferences preserve the fleet width and start both rails open', () => {
  assert.deepEqual(defaultRailPreferences(275), {
    fleet: { width: 275, collapsed: false },
    notes: { width: 280, collapsed: false },
  })
})

test('the utility rail stays hand-rolled and collapsed rails reserve no layout gutter', () => {
  const rail = readFileSync(new URL('../src/features/layout/UtilityRail.tsx', import.meta.url), 'utf8')
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  assert.doesNotMatch(rail, /dockview/i)
  assert.match(rail, /rail-toggle-collapsed/)
  assert.match(rail, /if \(collapsed\)/)
  assert.match(app, /<UtilityRail[\s\S]*side="left"[\s\S]*<section className="shell-main">[\s\S]*<UtilityRail[\s\S]*side="right"/)
  assert.doesNotMatch(controller, /toggleNotesRail[\s\S]{0,300}(?:pushState|replaceState|updateHistory)/)
})

test('reset cannot resurrect migrated v2 rail preferences', () => {
  const persistence = readFileSync(new URL('../src/features/layout/useLayoutPersistence.ts', import.meta.url), 'utf8')
  assert.match(persistence, /removeItem\(v2LayoutStorageKey\)/)
})
