import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('App is a small panel-kind-agnostic composition root', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  assert.ok(app.split('\n').length < 200)
  assert.doesNotMatch(app, /kind\s*===/)
  assert.match(app, /<DockviewReact/)
  assert.match(app, /<FleetSidebar/)
  assert.match(app, /<footer className="status-bar">/)
})

test('workspace actions and changing data have separate contexts', () => {
  const context = readFileSync(new URL('../src/features/workspace/workspaceContext.tsx', import.meta.url), 'utf8')
  assert.match(context, /WorkspaceActionsContext/)
  assert.match(context, /WorkspaceDataContext/)
  assert.doesNotMatch(context, /WorkspaceContext =/)
})

test('layout persistence owns restore, debounce, pagehide, and last-good writes', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const persistence = readFileSync(new URL('../src/features/layout/useLayoutPersistence.ts', import.meta.url), 'utf8')
  assert.doesNotMatch(app, /localStorage|persistableDockLayout|writeStoredLayout/)
  assert.match(persistence, /readStoredLayout\(localStorage\)/)
  assert.match(persistence, /window\.setTimeout\(flushLayout, 120\)/)
  assert.match(persistence, /useDOMEvent\(window, 'pagehide'/)
  assert.match(persistence, /writeStoredLayout\(localStorage/)
})
