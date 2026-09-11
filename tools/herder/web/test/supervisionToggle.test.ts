import assert from 'node:assert/strict'
import test from 'node:test'

import { primaryTreeRowClick, toggleTreeRow } from '../src/features/sidebar/sidebarInteractions.ts'

function managerItem(expandedItems: string[], previews: string[]) {
  const id = 'agent:manager'
  return {
    getId: () => id,
    setFocused: () => undefined,
    primaryAction: () => previews.push(id),
    isExpanded: () => expandedItems.includes(id),
    expand: () => expandedItems.push(id),
    collapse: () => expandedItems.splice(expandedItems.indexOf(id), 1),
  }
}

test('manager row primary click previews without changing expansion', () => {
  const expandedItems = ['agent:manager']
  const previews: string[] = []
  const selected: string[][] = []
  primaryTreeRowClick(managerItem(expandedItems, previews), (items) => selected.push(items))
  assert.deepEqual(previews, ['agent:manager'])
  assert.deepEqual(selected, [['agent:manager']])
  assert.deepEqual(expandedItems, ['agent:manager'])
})

test('manager chevron toggles expansion without previewing', () => {
  const expandedItems = ['agent:manager']
  const previews: string[] = []
  const item = managerItem(expandedItems, previews)
  toggleTreeRow(item)
  assert.deepEqual(expandedItems, [])
  toggleTreeRow(item)
  assert.deepEqual(expandedItems, ['agent:manager'])
  assert.deepEqual(previews, [])
})
