import assert from 'node:assert/strict'
import test from 'node:test'
import { fileTabID, pinFileTab, previewFileTab } from '../src/features/files/fileTabs.ts'

const first = { root: '/repo', path: 'src/App.tsx', line: 14 }
const second = { root: '/repo', path: 'README.md' }

test('file preview slot is replaceable and stable by opaque root plus path', () => {
  const opened = previewFileTab([], first)
  assert.equal(opened.length, 1)
  assert.equal(opened[0].preview, true)
  assert.equal(opened[0].id, fileTabID(first.root, first.path))
  assert.deepEqual(previewFileTab(opened, second).map((tab) => tab.path), ['README.md'])
})

test('pinned files survive later previews and reopening updates the requested line', () => {
  const pinned = pinFileTab(previewFileTab([], first), first)
  const withPreview = previewFileTab(pinned, second)
  assert.equal(withPreview.length, 2)
  assert.equal(withPreview[0].preview, false)
  const reopened = previewFileTab(withPreview, { ...first, line: 99 })
  assert.equal(reopened.find((tab) => tab.path === first.path)?.line, 99)
  assert.equal(reopened.find((tab) => tab.path === first.path)?.preview, false)
})
