import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('shell facts and global controls live only in the bottom status bar', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  assert.equal(app.match(/SSE:/g)?.length, 1)
  const footer = app.slice(app.indexOf('<footer className="status-bar">'), app.indexOf('</footer>'))
  assert.match(footer, /SSE:/)
  assert.match(footer, /layout: this browser/)
  assert.match(footer, /<ThemeToggle \/>/)
  assert.match(footer, /className="shortcut-button"/)
})

test('owner-ruled composer noise is removed without dropping accessibility or attribution truth', () => {
  const composer = readFileSync(new URL('../src/features/composer/Composer.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(composer, /Enter for newline|attribution-copy|web senders are not addressable bus peers/)
  assert.match(composer, /aria-label=\{`Message \$\{name\}`\}/)

  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  assert.match(app, /Web sends are attributed to this viewer; web senders are not addressable bus peers\./)
})

test('the physical Alt+W binding keeps the editable-target guard', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const shortcuts = readFileSync(new URL('../src/features/layout/shellShortcuts.ts', import.meta.url), 'utf8')
  assert.match(app, /bindShellShortcuts\(window/)
  assert.match(shortcuts, /'Alt\+KeyW': claimed\(actions\.closePanel, true\)/)
})

test('group headers expose maximize without the retired per-group quick-open button', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(app, /Quick open file or folder in this group|className="new-tab"/)
  assert.match(app, /aria-label=\{maximized \? 'Restore group' : 'Maximize group'\}/)
})

test('scroll shortcuts dispatch only to the active group viewport', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  assert.match(app, /const viewport = document\.querySelector\('\.dv-active-group \[data-follow-scroll\]'\)/)
  assert.match(app, /viewport\.dispatchEvent\(new CustomEvent\(followScrollCommandEvent/)
  assert.doesNotMatch(app, /window\.dispatchEvent\(new CustomEvent\(followScrollCommandEvent/)
})
