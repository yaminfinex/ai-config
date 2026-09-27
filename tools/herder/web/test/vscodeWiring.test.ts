import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8')

test('the agent context strip offers VS Code for the selected agent cwd', () => {
  const strip = source('features/transcript/AgentContextStrip.tsx')
  assert.match(strip, /import \{ VSCodeLink \} from '\.\.\/vscode\/index\.ts'/)
  assert.match(strip, /\{cwd && <VSCodeLink cwd=\{cwd\} \/>\}/)
})

test('the VS Code link is a plain anchor once mapped and asks just in time otherwise', () => {
  const link = source('features/vscode/VSCodeLink.tsx')
  assert.match(link, /const url = alias \? vscodeRemoteURL\(alias, cwd\) : null/)
  assert.match(link, /<a className="context-fact context-vscode" href=\{url\} aria-label=\{label\}/)
  assert.match(link, /const label = `Open \$\{folderName\(cwd\)\} in VS Code`/)
  assert.match(link, /onClick=\{\(\) => setPrompting\(true\)\}/)
  assert.match(link, /const saved = vscodeHosts\.set\(hostKey, value\)[\s\S]*if \(next\) window\.location\.href = next/)
  assert.match(link, /onCancel=\{\(\) => setPrompting\(false\)\}/)
  assert.match(link, /event\.key === 'Escape'/)
  assert.match(link, /wrap\.current\?\.querySelector<HTMLElement>\('a, button'\)\?\.focus\(\)/)
})

test('the alias form validates inline and stores nothing itself', () => {
  const form = source('features/vscode/HostAliasForm.tsx')
  assert.match(form, /const alias = validateHostAlias\(value\)\s*if \(!alias\.ok\) \{ setProblem\(alias\.reason\); return \}/)
  assert.match(form, /role="alert">\{problem\}/)
  assert.match(form, /~\/\.ssh\/config for this machine/)
  assert.doesNotMatch(form, /vscodeHosts/)
})

test('the status bar opens a settings panel with the VS Code section next to shortcuts and theme', () => {
  const app = source('App.tsx')
  assert.match(app, /onClick=\{onShortcuts\}>\?<\/button>\s*<button type="button" className="shortcut-button settings-button"[^>]*aria-label="Open settings"[^>]*onClick=\{onSettings\}>[\s\S]*?<\/button>\s*<ThemeToggle \/>/)
  assert.match(app, /<SettingsPanel open=\{settingsOpen\} onClose=\{\(\) => setSettingsOpen\(false\)\} \/>/)
  assert.match(app, /onSettings=\{\(\) => setSettingsOpen\(true\)\}/)
  const panel = source('features/settings/SettingsPanel.tsx')
  assert.match(panel, /<VSCodeSettings \/>/)
  assert.match(panel, /event\.key === 'Escape'[^}]*onClose\(\)/)
  assert.match(panel, /const previous = document\.activeElement[\s\S]*return \(\) => previous\?\.focus\(\)/)
  const settings = source('features/vscode/VSCodeSettings.tsx')
  assert.match(settings, /<h3 id="settings-vscode-title">VS Code Remote-SSH<\/h3>/)
  assert.match(settings, /vscodeHosts\.set\(hostKey, value\)/)
  assert.match(settings, /onClick=\{\(\) => \{ vscodeHosts\.remove\(hostKey\)/)
  assert.match(settings, /otherHosts\(hosts, hostKey\)[\s\S]*onClick=\{\(\) => vscodeHosts\.remove\(key\)\}/)
})
