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

test('per-event stream state is isolated from the workspace controller and dock contexts', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  const context = readFileSync(new URL('../src/features/workspace/workspaceContext.tsx', import.meta.url), 'utf8')
  const stream = readFileSync(new URL('../src/stream/useFleetStream.ts', import.meta.url), 'utf8')
  assert.match(app, /function StreamStatusBar/)
  assert.match(app, /function StreamBanners/)
  assert.match(stream, /export function useStreamStatus/)
  assert.match(stream, /export function useStreamAlerts/)
  assert.doesNotMatch(controller, /const stream\s*=\s*useFleetStream/)
  assert.doesNotMatch(controller, /streamProblems/)
  assert.doesNotMatch(context, /StreamState|streamProblems|stream:/)
})

test('stream state leaves the banner stack without removing its other problem sources', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const banners = app.slice(app.indexOf('function StreamBanners'), app.indexOf('function StreamStatusBar'))
  assert.match(banners, /Object\.entries\(problems\)\.filter\(\(\[source\]\) => source !== 'stream'\)/)
  assert.match(banners, /streamProblems\(stream\.problems, fleetProblem\)/)
  assert.match(banners, /spaceProblem && <Banner source="space" detail=\{spaceProblem\}/)
  assert.match(app, /return \{ \.\.\.problems, \.\.\.\(fleetProblem \? \{ fleet: fleetProblem \} : \{\}\) \}/)
})

test('the optional stream note renders on its status tick', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const tick = app.slice(app.indexOf('function StatusTick'), app.indexOf('function NotesCount'))
  assert.match(tick, /\{tick\.note && <span className="health-note">\{tick\.note\}<\/span>\}/)
})

test('browser persistence stays outside the App composition root', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const persistence = readFileSync(new URL('../src/features/layout/useLayoutPersistence.ts', import.meta.url), 'utf8')
  assert.doesNotMatch(app, /localStorage|persistableDockLayout|writeStoredLayout/)
  assert.match(persistence, /window\.setTimeout/)
  assert.match(persistence, /}, 120\)/)
  assert.match(persistence, /useDOMEvent\(window, 'pagehide'/)
  assert.match(persistence, /persistLayoutSnapshot/)
})

test('palette and tab menu share the exact workspace pane-send actions', () => {
  const palette = readFileSync(new URL('../src/features/files/QuickOpen.tsx', import.meta.url), 'utf8')
  const menu = readFileSync(new URL('../src/features/workspace/DockTabMenu.tsx', import.meta.url), 'utf8')
  for (const action of ['sendPanelToSpace', 'sendPanelToNewSpace']) {
    assert.match(palette, new RegExp(`workspaceActions\\.${action}\\(`))
    assert.match(menu, new RegExp(`actions\\.${action}\\(`))
  }
})
