import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import type { DockPanelParams } from '../src/features/layout/dockLayout.ts'
import {
  decideHistoryUpdate,
  createHistorySuppressor,
  historyEntryForPanel,
  layoutRouteState,
  routeFromHistory,
  routeFromLocation,
  shouldReplayInitialRoute,
} from '../src/features/layout/historyModel.ts'

const agent = (name = 'mavu'): DockPanelParams => ({ kind: 'agent', name, preview: true })
const file = (path = 'README.md', line?: number): DockPanelParams => ({
  kind: 'file', root: '/repo root', path, ...(line ? { line } : {}), preview: true, viewMode: 'rendered',
})
const folder = (): DockPanelParams => ({ kind: 'folder', root: '/repo root', path: 'src/lib', preview: true })
const changes = (): DockPanelParams => ({ kind: 'changes', root: '/repo root', preview: true })
const screen = (): DockPanelParams => ({
  kind: 'screen',
  pane: { pane_id: 'pane-1', agent: 'mavu', tool: 'codex', herdr_status: 'active', bus_status: 'active', gap: '' },
  identity: { paneID: 'pane-1', workspaceID: 'workspace-1', tabID: 'tab-1', agent: 'mavu' },
  preview: true,
})

test('every addressable panel kind has a canonical URL and round-trips through its registry validator', () => {
  const cases: Array<[DockPanelParams, string]> = [
    [agent('mavu/tag'), '/agents/mavu%2Ftag'],
    [file('docs/read me.md', 17), '/file?root=%2Frepo+root&path=docs%2Fread+me.md&line=17'],
    [folder(), '/folder?root=%2Frepo+root&path=src%2Flib'],
    [changes(), '/changes?root=%2Frepo+root'],
  ]
  for (const [params, expectedPath] of cases) {
    const entry = historyEntryForPanel(params)
    assert.equal(entry.path, expectedPath)
    const url = new URL(expectedPath, 'http://herder.test')
    const route = routeFromLocation(url.pathname, url.search)
    assert.equal(route.page, 'panel')
    if (route.page === 'panel') assert.equal(route.params.kind, params.kind)
  }
})

test('HTML deep links default rendered unless a line anchor requests source', () => {
  const rendered = routeFromLocation('/file', '?root=%2Frepo&path=mockup.html')
  assert.equal(rendered.page, 'panel')
  if (rendered.page === 'panel') assert.equal(rendered.params.kind === 'file' ? rendered.params.viewMode : undefined, 'rendered')

  const source = routeFromLocation('/file', '?root=%2Frepo&path=mockup.html&line=8')
  assert.equal(source.page, 'panel')
  if (source.page === 'panel') assert.equal(source.params.kind === 'file' ? source.params.viewMode : undefined, 'source')
})

test('screen entries keep their validated subject in state while the URL asserts only the workspace', () => {
  const entry = historyEntryForPanel(screen())
  assert.equal(entry.path, '/')
  assert.equal('preview' in (entry.state.subject as object), false)
  const route = routeFromHistory('/', '', entry.state)
  assert.equal(route.page, 'panel')
  if (route.page === 'panel') {
    assert.equal(route.params.kind, 'screen')
    assert.equal(route.params.preview, true)
  }
})

test('history subjects omit pin and view state but retain a file line opening hint', () => {
  const entry = historyEntryForPanel({ ...file('README.md', 23), preview: false, viewMode: 'source' })
  assert.deepEqual(entry.state, {
    ...layoutRouteState,
    subject: { kind: 'file', root: '/repo root', path: 'README.md', line: 23 },
  })
  const route = routeFromHistory('/file', '?root=%2Frepo+root&path=README.md&line=23', entry.state)
  assert.equal(route.page, 'panel')
  if (route.page === 'panel') assert.deepEqual(route.params, {
    kind: 'file', root: '/repo root', path: 'README.md', line: 23, preview: true, viewMode: 'source',
  })
})

test('folder history and popstate replay cannot carry a transient selection hint', () => {
  const hinted = { ...folder(), selectionHint: { root: '/repo root', path: 'src/lib/model.ts' } }
  const entry = historyEntryForPanel(hinted)
  assert.deepEqual(entry.state, {
    ...layoutRouteState,
    subject: { kind: 'folder', root: '/repo root', path: 'src/lib' },
  })
  const route = routeFromHistory('/folder', '?root=%2Frepo+root&path=src%2Flib', {
    ...layoutRouteState,
    subject: { kind: 'folder', root: '/repo root', path: 'src/lib', selectionHint: hinted.selectionHint },
  })
  assert.equal(route.page, 'panel')
  if (route.page === 'panel') assert.deepEqual(route.params, folder())
})

test('known paths with malformed params and unknown paths are honest missing routes', () => {
  for (const [pathname, search] of [
    ['/file', '?root=%2Frepo'],
    ['/file', '?root=%2Frepo&path=README.md&line=0'],
    ['/folder', '?root=&path=src'],
    ['/changes', ''],
    ['/unknown', ''],
  ]) assert.deepEqual(routeFromLocation(pathname, search), { page: 'missing' })
  assert.deepEqual(routeFromLocation('/', ''), { page: 'shell' })
})

test('history replay rejects unknown or malformed state instead of resurrecting it', () => {
  assert.deepEqual(routeFromHistory('/', '', { ...layoutRouteState, subject: { kind: 'notes', id: 'ghost' } }), { page: 'shell' })
  assert.deepEqual(routeFromHistory('/', '', { ...layoutRouteState, subject: { kind: 'screen', pane: {}, identity: {} } }), { page: 'shell' })
  assert.equal(routeFromHistory('/agents/mavu', '', { ...layoutRouteState, subject: { kind: 'agent', name: '' } }).page, 'panel')
})

test('a restored layout wins over an app-created route while deliberate deep links replay', () => {
  const panelRoute = routeFromLocation('/agents/mavu', '')
  assert.equal(shouldReplayInitialRoute(panelRoute, layoutRouteState, true), false)
  assert.equal(shouldReplayInitialRoute(panelRoute, null, true), true)
  assert.equal(shouldReplayInitialRoute(panelRoute, {}, true), true)
  assert.equal(shouldReplayInitialRoute(panelRoute, layoutRouteState, false), true)
  assert.equal(shouldReplayInitialRoute({ page: 'shell' }, layoutRouteState, true), false)
})

test('the history decision table pushes only distinct unsuppressed user activations', () => {
  const current = historyEntryForPanel(agent('mavu')).state
  const rows = [
    [{ cause: 'activation', suppressed: false, next: agent('nilo') }, 'push'],
    [{ cause: 'activation', suppressed: false, next: agent('mavu') }, 'replace'],
    [{ cause: 'merge', suppressed: false, next: agent('nilo') }, 'replace'],
    [{ cause: 'stamp', suppressed: false, next: agent('nilo') }, 'replace'],
    [{ cause: 'replay', suppressed: false, next: agent('nilo') }, 'replace'],
    [{ cause: 'activation', suppressed: true, next: agent('nilo') }, 'replace'],
  ] as const
  for (const [input, expected] of rows) {
    assert.equal(decideHistoryUpdate(current, input.next, input.cause, input.suppressed).method, expected)
  }
})

test('file line retargets dedupe by subject identity and replace the current entry', () => {
  const current = historyEntryForPanel(file('README.md', 10)).state
  const update = decideHistoryUpdate(current, file('README.md', 20), 'activation', false)
  assert.equal(update.method, 'replace')
  assert.deepEqual(update.entry.state.subject, { kind: 'file', root: '/repo root', path: 'README.md', line: 20 })
})

test('suppression windows nest and stay closed until each scheduled frame releases', () => {
  const frames: Array<() => void> = []
  const cancelled: number[] = []
  const suppressor = createHistorySuppressor((callback) => {
    frames.push(callback)
    return frames.length
  }, (handle) => cancelled.push(handle))

  suppressor.run(() => {
    assert.equal(suppressor.active(), true)
    suppressor.run(() => assert.equal(suppressor.active(), true))
  })
  assert.equal(suppressor.active(), true)
  assert.equal(frames.length, 2)
  frames[0]()
  assert.equal(suppressor.active(), true)
  frames[1]()
  assert.equal(suppressor.active(), false)

  suppressor.run(() => undefined)
  suppressor.dispose()
  assert.deepEqual(cancelled, [3])
  assert.equal(suppressor.active(), false)
})

test('every close and replay path enters suppression before Dockview can activate a neighbor', () => {
  const actions = readFileSync(new URL('../src/features/workspace/useWorkspaceActions.ts', import.meta.url), 'utf8')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  const registry = readFileSync(new URL('../src/features/workspace/panelRegistry.tsx', import.meta.url), 'utf8')
  const shortcuts = readFileSync(new URL('../src/features/workspace/useWorkspaceShortcuts.ts', import.meta.url), 'utf8')
  assert.match(actions, /withHistorySuppressed\(\(\) => panel\.api\.close\(\)\)/)
  assert.match(actions, /withHistorySuppressed\(\(\) => api\.removePanel\(replaced\)\)/)
  assert.match(actions, /withHistorySuppressed\(\(\) => api\.clear\(\)\)/)
  assert.match(registry, /actions\.closePanel\(api\.id\)/)
  assert.match(shortcuts, /closePanel\(panel\.id\)/)
  assert.match(controller, /historySuppressor\.run\(\(\) => \{[\s\S]*openPanel\(\{ \.\.\.route\.params, preview: true \}, undefined, false\)/)
  assert.match(controller, /activePanel: \(\{ panel \}\) => \{\s*updateHistory\(panelParams\(panel\?\.params\) \?\? undefined, 'activation'\)/)
})
