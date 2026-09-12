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
  createFolderTreePreferencesStore,
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

function fakeStorage() {
  const store = new Map<string, string>()
  return { store, getItem: (key: string) => store.get(key) ?? null, setItem: (key: string, raw: string) => { store.set(key, raw) } }
}

test('the store is the truth for the document: a throwing write keeps the session value', () => {
  const throwing = { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') } }
  const store = createFolderTreePreferencesStore(() => throwing)
  assert.deepEqual(store.get(), { version: 1, width: 220, hidden: false })
  assert.deepEqual(store.set({ width: 150 }), { version: 1, width: 150, hidden: false })
  assert.deepEqual(store.set({ hidden: true }), { version: 1, width: 150, hidden: true })
  assert.deepEqual(store.set({ hidden: false }), { version: 1, width: 150, hidden: false })
  const accessorThrows = createFolderTreePreferencesStore(() => { throw new Error('no storage') })
  assert.deepEqual(accessorThrows.set({ width: 150 }), { version: 1, width: 150, hidden: false })
  assert.deepEqual(accessorThrows.set({ hidden: true }), { version: 1, width: 150, hidden: true })
})

test('a valid but stale stored value after a failed write never resets the session width', () => {
  const storage = fakeStorage()
  storage.store.set(folderTreeStorageKey, '{"version":1,"width":260,"hidden":false}')
  let writable = true
  const flaky = { getItem: storage.getItem, setItem: (key: string, raw: string) => { if (!writable) throw new Error('quota'); storage.setItem(key, raw) } }
  const store = createFolderTreePreferencesStore(() => flaky)
  assert.equal(store.get().width, 260, 'initial value comes from storage')
  writable = false
  assert.equal(store.set({ width: 150 }).width, 150)
  assert.equal(storage.store.get(folderTreeStorageKey), '{"version":1,"width":260,"hidden":false}', 'storage is stale')
  assert.deepEqual(store.set({ hidden: true }), { version: 1, width: 150, hidden: true })
  assert.deepEqual(store.set({ hidden: false }), { version: 1, width: 150, hidden: false })
  writable = true
  store.set({ hidden: true })
  assert.equal(storage.store.get(folderTreeStorageKey), '{"version":1,"width":150,"hidden":true}', 'the next good write mirrors the session value')
})

test('two subscribers share one value, and unsubscribing stops notifications', () => {
  const storage = fakeStorage()
  const store = createFolderTreePreferencesStore(() => storage)
  const seenA: number[] = []
  const seenB: number[] = []
  const stopA = store.subscribe(() => seenA.push(store.get().width))
  const stopB = store.subscribe(() => seenB.push(store.get().width))
  store.set({ width: 150 })
  assert.deepEqual(seenA, [150])
  assert.deepEqual(seenB, [150])
  assert.deepEqual(store.get(), { version: 1, width: 150, hidden: false })
  stopA()
  store.set({ hidden: true })
  assert.deepEqual(seenA, [150], 'A no longer hears')
  assert.deepEqual(seenB, [150, 150])
  assert.deepEqual(store.get(), { version: 1, width: 150, hidden: true })
  stopB()
  store.set({ hidden: false })
  assert.deepEqual(seenB, [150, 150])
  assert.equal(storage.store.get(folderTreeStorageKey), '{"version":1,"width":150,"hidden":false}')
})

test('the store reads storage lazily once: valid stored value wins, otherwise defaults', () => {
  const valid = fakeStorage()
  valid.store.set(folderTreeStorageKey, '{"version":1,"width":300,"hidden":true}')
  assert.deepEqual(createFolderTreePreferencesStore(() => valid).get(), { version: 1, width: 300, hidden: true })
  const wrongVersion = fakeStorage()
  wrongVersion.store.set(folderTreeStorageKey, '{"version":2,"width":300}')
  assert.deepEqual(createFolderTreePreferencesStore(() => wrongVersion).get(), { version: 1, width: 220, hidden: false })
  const garbage = fakeStorage()
  garbage.store.set(folderTreeStorageKey, 'nope')
  assert.deepEqual(createFolderTreePreferencesStore(() => garbage).get(), { version: 1, width: 220, hidden: false })
  assert.deepEqual(createFolderTreePreferencesStore(() => null).get(), { version: 1, width: 220, hidden: false })
  let reads = 0
  const counting = { getItem: () => { reads++; return null }, setItem: () => undefined }
  const store = createFolderTreePreferencesStore(() => counting)
  store.get(); store.get(); store.set({ width: 200 }); store.get()
  assert.equal(reads, 1, 'storage is read once, never re-read after a set')
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
