import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  clampFolderTreeWidth,
  folderTreeMaxWidth,
  folderTreePreferencesValue,
  folderTreeStorageKey,
  folderTreeWidthFromKey,
  parseFolderTreePreferences,
  readFolderTreePreferences,
  resizedFolderTreeWidth,
  updateFolderTreePreferences,
  writeFolderTreePreferences,
} from '../src/features/folders/folderTreeModel.ts'

test('folder tree width is bounded to 150 px and 60% of the panel', () => {
  assert.equal(folderTreeMaxWidth(1000), 600)
  assert.equal(folderTreeMaxWidth(0), Number.POSITIVE_INFINITY)
  assert.equal(clampFolderTreeWidth(20, 1000), 150)
  assert.equal(clampFolderTreeWidth(900, 1000), 600)
  assert.equal(clampFolderTreeWidth(300.4, 1000), 300)
  assert.equal(clampFolderTreeWidth(Number.NaN, 1000), 220)
  // a panel narrower than the minimum still never drops below the minimum
  assert.equal(clampFolderTreeWidth(500, 200), 150)
})

test('drag deltas and keys move the width inside the bounds', () => {
  assert.equal(resizedFolderTreeWidth(220, 35, 1000), 255)
  assert.equal(resizedFolderTreeWidth(220, -500, 1000), 150)
  assert.equal(folderTreeWidthFromKey(220, 'ArrowRight', 1000), 230)
  assert.equal(folderTreeWidthFromKey(220, 'ArrowLeft', 1000), 210)
  assert.equal(folderTreeWidthFromKey(220, 'Home', 1000), 150)
  assert.equal(folderTreeWidthFromKey(220, 'End', 1000), 600)
  assert.equal(folderTreeWidthFromKey(220, 'ArrowUp', 1000), null)
})

test('folder tree preferences round-trip through a versioned key and reject bad shapes', () => {
  const value = folderTreePreferencesValue(240.6, true)
  assert.deepEqual(value, { version: 1, width: 241, hidden: true })
  assert.deepEqual(parseFolderTreePreferences(JSON.stringify(value)), value)
  assert.deepEqual(parseFolderTreePreferences('{"version":1,"width":300}'), { version: 1, width: 300, hidden: false })
  assert.equal(parseFolderTreePreferences('{"version":2,"width":300}'), null)
  assert.equal(parseFolderTreePreferences('{"version":1,"width":"300"}'), null)
  assert.equal(parseFolderTreePreferences('{"version":1,"width":300,"hidden":"yes"}'), null)
  assert.equal(parseFolderTreePreferences('[]'), null)
  assert.equal(parseFolderTreePreferences('nope'), null)
  assert.equal(parseFolderTreePreferences(null), null)
  assert.equal(parseFolderTreePreferences('{"version":1,"width":10}')?.width, 150)
})

test('storage helpers fall back to defaults and swallow storage failures', () => {
  const store = new Map<string, string>()
  const storage = { getItem: (key: string) => store.get(key) ?? null, setItem: (key: string, raw: string) => { store.set(key, raw) } }
  assert.deepEqual(readFolderTreePreferences(storage), { version: 1, width: 220, hidden: false })
  writeFolderTreePreferences(storage, folderTreePreferencesValue(260, false))
  assert.equal(store.get(folderTreeStorageKey), '{"version":1,"width":260,"hidden":false}')
  assert.deepEqual(readFolderTreePreferences(storage), { version: 1, width: 260, hidden: false })
  assert.deepEqual(readFolderTreePreferences(null), { version: 1, width: 220, hidden: false })
  const throwing = { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') } }
  assert.deepEqual(readFolderTreePreferences(throwing), { version: 1, width: 220, hidden: false })
  assert.doesNotThrow(() => writeFolderTreePreferences(throwing, folderTreePreferencesValue(260, false)))
})

test('preference patches merge against the stored value so two panels never clobber each other', () => {
  const store = new Map<string, string>()
  const storage = { getItem: (key: string) => store.get(key) ?? null, setItem: (key: string, raw: string) => { store.set(key, raw) } }
  // panel A presses Home (width 150); panel B, still holding its mount-time width, hides the tree
  assert.deepEqual(updateFolderTreePreferences(storage, { width: 150 }), { version: 1, width: 150, hidden: false })
  assert.deepEqual(updateFolderTreePreferences(storage, { hidden: true }), { version: 1, width: 150, hidden: true })
  assert.deepEqual(updateFolderTreePreferences(storage, { hidden: false }), { version: 1, width: 150, hidden: false })
  assert.equal(store.get(folderTreeStorageKey), '{"version":1,"width":150,"hidden":false}')
  assert.deepEqual(updateFolderTreePreferences(null, { width: 300 }), { version: 1, width: 300, hidden: false })
})

test('folder workspace grid is declared once and the row action stays keyboard-reachable (CSS source guards)', () => {
  const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  const rules = [...styles.matchAll(/^\s*([^{}\n]+)\{([^}]*)\}/gmu)].map((m) => ({ selector: m[1]!.trim(), body: m[2]!, indented: /^\s/u.test(m[0]) }))
  const workspace = rules.filter((rule) => /\.folder-workspace(?![\w-])/u.test(rule.selector) && /grid-template-columns/u.test(rule.body))
  assert.deepEqual(workspace.map((rule) => rule.selector), ['.folder-workspace', '.folder-workspace.tree-hidden'])
  assert.equal(workspace.filter((rule) => !rule.selector.includes('.tree-hidden')).length, 1, 'exactly one grid declaration outside .tree-hidden')
  for (const block of styles.matchAll(/@media[^{]*\{([\s\S]*?)\n\}/gu)) assert.doesNotMatch(block[1]!, /\.folder-workspace/u, 'no folder-workspace override inside @media')
  assert.match(styles, /^\.folder-workspace \{[^}]*grid-template-columns: var\(--folder-tree-width\)/mu)
  const action = rules.find((rule) => rule.selector === '.folder-row-action')
  assert.ok(action, '.folder-row-action rule present')
  assert.match(action.body, /opacity: 0/u)
  assert.doesNotMatch(action.body, /display: none/u)
  assert.doesNotMatch(styles, /\.folder-row-action[^{]*\{[^}]*display: none/u)
  assert.doesNotMatch(styles, /\.folder-row-action[^{]*\{[^}]*visibility: hidden/u)
  assert.match(styles, /\.panel-tree-row:focus-within \.folder-row-action[^{]*\{[^}]*opacity: 1/u)
})
