import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { treeChildIndex, treeIndent, treeKeyIntent, treeParentIndex } from '../src/shared/treeModel.ts'

test('shared tree indentation uses the fleet density contract', () => {
  assert.equal(treeIndent(-1), 5)
  assert.equal(treeIndent(0), 5)
  assert.equal(treeIndent(1), 21)
  assert.equal(treeIndent(3), 53)
})

test('shared tree keys distinguish navigation, expansion, and primary action', () => {
  assert.equal(treeKeyIntent('ArrowUp', false, false), 'previous')
  assert.equal(treeKeyIntent('ArrowDown', false, false), 'next')
  assert.equal(treeKeyIntent('Home', false, false), 'first')
  assert.equal(treeKeyIntent('End', false, false), 'last')
  assert.equal(treeKeyIntent('ArrowRight', true, false), 'expand')
  assert.equal(treeKeyIntent('ArrowRight', true, true), 'child')
  assert.equal(treeKeyIntent('ArrowRight', false, false), null)
  assert.equal(treeKeyIntent('ArrowLeft', true, true), 'collapse')
  assert.equal(treeKeyIntent('ArrowLeft', true, false), 'parent')
  assert.equal(treeKeyIntent('ArrowLeft', false, false), 'parent')
  assert.equal(treeKeyIntent('Enter', false, false), 'primary')
  assert.equal(treeKeyIntent(' ', false, false), 'primary')
  assert.equal(treeKeyIntent('Escape', false, false), null)
})

test('shared tree parent and child movement respects visible levels', () => {
  const levels = [1, 2, 3, 2, 1]
  assert.equal(treeChildIndex(levels, 0), 1)
  assert.equal(treeChildIndex(levels, 1), 2)
  assert.equal(treeChildIndex(levels, 2), -1)
  assert.equal(treeParentIndex(levels, 2), 1)
  assert.equal(treeParentIndex(levels, 3), 0)
  assert.equal(treeParentIndex(levels, 0), -1)
  assert.equal(treeParentIndex(levels, 99), -1)
})

test('fleet and folder features share one row, state, and disclosure idiom', () => {
  const fleet = readFileSync(new URL('../src/features/sidebar/FleetSidebar.tsx', import.meta.url), 'utf8')
  const folders = readFileSync(new URL('../src/features/folders/FolderPanel.tsx', import.meta.url), 'utf8')
  const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  assert.match(fleet, /<TreeRow/)
  assert.match(fleet, /<TreeState/)
  assert.match(folders, /<TreeRow/)
  assert.match(folders, /<TreeState/)
  assert.match(folders, /role="tree"/)
  assert.doesNotMatch(styles, /\.folder-tree-row|\.folder-disclosure|\.folder-tree-state|\.folder-tree-error/)
  assert.doesNotMatch(fleet, /className="tree-row"|className=\{`tree-row/)
  assert.doesNotMatch(folders, /className="folder-tree-row"|className=\{`folder-tree-row/)
})
