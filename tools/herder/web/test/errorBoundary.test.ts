import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('the root error boundary shows only the error and a reload action', () => {
  const boundary = read('../src/shared/ErrorBoundary.tsx')
  assert.match(boundary, /role="alert"/)
  assert.match(boundary, /<strong>\{this\.props\.context \?\? 'Herder interface'\} failed<\/strong>/)
  assert.match(boundary, /this\.state\.error instanceof Error && this\.state\.error\.message && <p>\{this\.state\.error\.message\}<\/p>/)
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

test('the status bar is the fixed second shell row outside the centre column', () => {
  const app = read('../src/App.tsx')
  const styles = read('../src/styles.css')
  const shellMain = app.slice(app.indexOf('<section className="shell-main">'), app.indexOf('</section>') + '</section>'.length)
  assert.doesNotMatch(shellMain, /<StreamStatusBar/)
  assert.match(app, /<\/div>\s+<StreamStatusBar/)
  assert.match(styles, /\.app-shell \{[^}]*grid-template-rows: minmax\(0, 1fr\) 24px;/)
})
