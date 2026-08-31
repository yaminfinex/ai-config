import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('shell facts and global controls live only in the bottom status bar', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const footer = app.slice(app.indexOf('<footer className="status-bar">'), app.indexOf('</footer>'))
  assert.match(footer, /health\.map/)
  assert.match(footer, /user:/)
  assert.match(footer, /last event:/)
  assert.doesNotMatch(footer, /layout: this browser|viewer:|>attributed<|stream\.messages|\{workspace\.stream\.messages\}/)
  assert.match(footer, /<ThemeToggle \/>/)
  assert.match(footer, /className="shortcut-button"/)
  assert.match(footer, /workspace-switcher-slot/)
  assert.match(footer, /<RailStatusToggle side="left"[\s\S]*<RailStatusToggle side="right"/)
  assert.match(app, /function NotesCount\(\)[\s\S]*useNotes\(\)[\s\S]*<NotesCount \/>/)
  const shell = app.slice(app.indexOf('function Shell'), app.indexOf('export default function App'))
  assert.doesNotMatch(shell, /useNotes\(\)/)
})

test('owner-ruled composer noise is removed without dropping accessibility or attribution truth', () => {
  const composer = readFileSync(new URL('../src/features/composer/Composer.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(composer, /Enter for newline|attribution-copy|web senders are not addressable bus peers/)
  assert.match(composer, /aria-label=\{`Message \$\{name\}`\}/)

  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  assert.match(app, /Web sends are attributed to this user; web senders are not addressable bus peers\./)
})

test('the physical Alt+W binding keeps the editable-target guard', () => {
  const shortcutsHook = readFileSync(new URL('../src/features/workspace/useWorkspaceShortcuts.ts', import.meta.url), 'utf8')
  const shortcuts = readFileSync(new URL('../src/features/layout/shellShortcuts.ts', import.meta.url), 'utf8')
  assert.match(shortcutsHook, /bindShellShortcuts\(window/)
  assert.match(shortcuts, /'Alt\+KeyW': claimed\(actions\.closePanel, true\)/)
})

test('group headers expose maximize without the retired per-group quick-open button', () => {
  const chrome = readFileSync(new URL('../src/features/workspace/workspaceChrome.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(chrome, /Quick open file or folder in this group|className="new-tab"/)
  assert.match(chrome, /aria-label=\{maximized \? 'Restore group' : 'Maximize group'\}/)
})

test('scroll shortcuts dispatch only to the active group viewport', () => {
  const shortcuts = readFileSync(new URL('../src/features/workspace/useWorkspaceShortcuts.ts', import.meta.url), 'utf8')
  assert.match(shortcuts, /const viewport = document\.querySelector\('\.dv-active-group \[data-follow-scroll\]'\)/)
  assert.match(shortcuts, /viewport\.dispatchEvent\(new CustomEvent\(followScrollCommandEvent/)
  assert.doesNotMatch(shortcuts, /window\.dispatchEvent\(new CustomEvent\(followScrollCommandEvent/)
})
