import assert from 'node:assert/strict'
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
