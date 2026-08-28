import assert from 'node:assert/strict'
import test from 'node:test'
import { isQuickOpenShortcut } from '../src/features/files/fileShortcut.ts'

test('quick open owns Ctrl/Cmd+K and adds only the ruled macOS Cmd+/ fallback', () => {
  assert.equal(isQuickOpenShortcut({ key: 'k', ctrlKey: true, metaKey: false }, 'Firefox Linux'), true)
  assert.equal(isQuickOpenShortcut({ key: 'K', ctrlKey: false, metaKey: true }, 'Chrome Mac'), true)
  assert.equal(isQuickOpenShortcut({ key: '/', ctrlKey: false, metaKey: true }, 'Chrome Mac'), true)
  assert.equal(isQuickOpenShortcut({ key: '/', ctrlKey: true, metaKey: false }, 'Chrome Linux'), false)
  assert.equal(isQuickOpenShortcut({ key: '/', ctrlKey: false, metaKey: true }, 'Chrome Linux'), false)
})
