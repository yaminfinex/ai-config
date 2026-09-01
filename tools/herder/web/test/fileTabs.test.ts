import assert from 'node:assert/strict'
import test from 'node:test'
import { closeFileTab, fileTabID, isHtmlPath, isMarkdownPath, pinFileTab, previewFileTab, setFileTabViewMode } from '../src/features/files/fileTabs.ts'

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

test('markdown detection is case-insensitive and includes long extensions', () => {
  assert.equal(isMarkdownPath('README.md'), true)
  assert.equal(isMarkdownPath('notes.MarkDown'), true)
  assert.equal(isMarkdownPath('notes.md.txt'), false)
  assert.equal(isMarkdownPath('src/App.tsx'), false)
})

test('HTML detection is case-insensitive and excludes compound suffixes', () => {
  assert.equal(isHtmlPath('notes-v1-mockup.html'), true)
  assert.equal(isHtmlPath('legacy.HTM'), true)
  assert.equal(isHtmlPath('preview.html.txt'), false)
  assert.equal(isHtmlPath('page.xhtml'), false)
  assert.equal(isHtmlPath('image.svg'), false)
})

test('markdown tabs default rendered while line targets force source', () => {
  const rendered = previewFileTab([], second)
  assert.equal(rendered[0].viewMode, 'rendered')

  const targeted = previewFileTab(rendered, { ...second, line: 12 })
  assert.equal(targeted[0].viewMode, 'source')
  assert.equal(targeted[0].line, 12)
})

test('HTML tabs default rendered while line targets force source', () => {
  const html = { root: '/repo', path: 'notes-v1-mockup.html' }
  const rendered = previewFileTab([], html)
  assert.equal(rendered[0].viewMode, 'rendered')

  const targeted = previewFileTab(rendered, { ...html, line: 12 })
  assert.equal(targeted[0].viewMode, 'source')
})

test('file view mode is per-tab and closing drops its state', () => {
  const tabs = previewFileTab(pinFileTab([], first), second)
  const rendered = tabs.find((tab) => tab.path === second.path)
  assert.ok(rendered)
  const toggled = setFileTabViewMode(tabs, rendered.id, 'source')
  assert.equal(toggled.find((tab) => tab.id === rendered.id)?.viewMode, 'source')
  assert.equal(toggled.find((tab) => tab.path === first.path)?.viewMode, 'source')
  assert.equal(setFileTabViewMode(toggled, rendered.id, 'source'), toggled)
  assert.equal(closeFileTab(toggled, 'agent:not-a-file-tab'), toggled)
  assert.deepEqual(closeFileTab(toggled, rendered.id).map((tab) => tab.id), [fileTabID(first.root, first.path)])
})
