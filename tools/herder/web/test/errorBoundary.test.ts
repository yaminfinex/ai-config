import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('the root error boundary shows only the error and a reload action', () => {
  const boundary = read('../src/shared/ErrorBoundary.tsx')
  assert.match(boundary, /role="alert"/)
  assert.match(boundary, /this\.state\.error\.message/)
  assert.match(boundary, />Reload<\/button>/)
  assert.match(boundary, /window\.location\.reload\(\)/)
})

test('App keeps the shell inside the root error boundary', () => {
  const app = read('../src/App.tsx')
  assert.match(app, /<NotesProvider><ErrorBoundary><Shell initialRoute=\{route\} \/><\/ErrorBoundary><\/NotesProvider>/)
})

test('each dock panel body has a kind-labelled boundary outside its component', () => {
  const registry = read('../src/features/workspace/panelRegistry.tsx')
  assert.match(registry, /<ErrorBoundary context=\{panelKindLabel\(kind as PanelKind\)\}><Panel \{\.\.\.props\} \/><\/ErrorBoundary>/)
  assert.match(registry, /Object\.entries\(panelRegistry\)\.map\(\(\[kind, descriptor\]\)/)
})
