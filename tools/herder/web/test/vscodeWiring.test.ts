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
  assert.match(link, /<ModalDialog [^>]*dialogRef=\{dialog\}\s*onClose=\{\(\) => \{ refocus\.current = true; setPrompting\(false\) \}\}>/)
  assert.match(link, /onCancel=\{\(\) => dialog\.current\?\.close\(\)\}/)
  assert.match(link, /const saved = vscodeHosts\.set\(hostKey, value\)\s*dialog\.current\?\.close\(\)/)
  assert.match(link, /if \(!refocus\.current\) return[\s\S]*wrap\.current\?\.querySelector<HTMLElement>\('a, button'\)\?\.focus\(\)/)
  assert.doesNotMatch(link, /createPortal|'Escape'/)
})

test('the VS Code control is the icon alone, named and titled by the label in both states', () => {
  const link = source('features/vscode/VSCodeLink.tsx')
  assert.match(link, /const vscodeMark = <svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true" focusable="false">\s*<path d="M23\.15 /)
  assert.match(link, /<a className="context-fact context-vscode" href=\{url\} aria-label=\{label\} title=\{label\}>\{vscodeMark\}<\/a>/)
  assert.match(link, /<button type="button" className="context-fact context-vscode" aria-label=\{label\} aria-haspopup="dialog" title=\{label\} onClick=\{\(\) => setPrompting\(true\)\}>\{vscodeMark\}<\/button>/)
  assert.doesNotMatch(link, />VS Code<|↗/)
  const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  assert.match(styles, /\.context-vscode svg \{ display: block; fill: currentColor; \}/)
  assert.match(styles, /\.context-vscode:hover, \.context-vscode:focus-visible \{ border-color: var\(--accent\); background: var\(--accent-dim\); \}/)
})

test('modal dialogs are native: showModal gives the focus trap, Escape and focus return', () => {
  const dialog = source('shared/ModalDialog.tsx')
  assert.match(dialog, /<dialog className=\{`modal-dialog \$\{className\}`\} aria-labelledby=\{labelledBy\} onClose=\{onClose\}/)
  assert.match(dialog, /if \(!node \|\| node\.open\) return\s*node\.showModal\(\)/)
  assert.match(dialog, /if \(!inside\) dialog\.close\(\)/)
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
  assert.match(panel, /<ModalDialog className="shortcut-reference settings-panel" labelledBy="settings-title" onClose=\{onClose\}>/)
  assert.match(panel, /<form method="dialog"><button type="submit" aria-label="Close settings">/)
  const settings = source('features/vscode/VSCodeSettings.tsx')
  assert.match(settings, /<h3 id="settings-vscode-title">VS Code Remote-SSH<\/h3>/)
  assert.match(settings, /const result = vscodeHosts\.set\(hostKey, value\)\s*if \(result\.ok\) setSaved\(result\.persisted \? [^:]+ : `[^`]*\$\{sessionOnly\}\.`\)/)
  assert.match(settings, /const persisted = vscodeHosts\.remove\(key\)\s*setSaved\(persisted \? `\$\{done\}\.` : `\$\{done\} \$\{sessionOnly\}\.`\)\s*input\.current\?\.focus\(\)/)
  assert.match(settings, /onClick=\{\(\) => remove\(hostKey, /)
  assert.match(settings, /otherHosts\(hosts, hostKey\)[\s\S]*onClick=\{\(\) => remove\(key, /)
  // The form is not keyed by the alias: a remount would drop focus to <body>.
  assert.match(settings, /<HostAliasForm hostKey=\{hostKey\} initial=\{alias\}/)
  assert.doesNotMatch(settings, /<HostAliasForm key=/)
  const form = source('features/vscode/HostAliasForm.tsx')
  assert.match(form, /if \(seen !== initial\) \{ setSeen\(initial\); setValue\(initial\); setProblem\(''\) \}/)
})
