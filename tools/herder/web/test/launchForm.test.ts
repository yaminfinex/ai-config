import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { dialogTabTargetIndex, initialLaunchForm, launchConfirmation, launchRefusal } from '../src/features/launch/launchModel.ts'

test('launch form starts with plain defaults and curated models', () => {
  assert.deepEqual(initialLaunchForm(), {
    tool: 'claude',
    model: 'opus',
    modelOptions: ['opus', 'sonnet', 'haiku'],
    tag: 'impl',
    repo: '',
    branch: '',
    branchHelp: 'A fresh worktree is created for the agent.',
  })
  assert.deepEqual(initialLaunchForm('codex'), {
    tool: 'codex',
    model: '',
    modelOptions: ['gpt-5.4', 'gpt-5.4-mini'],
    tag: 'impl',
    repo: '',
    branch: '',
    branchHelp: 'A fresh worktree is created for the agent.',
  })
})

test('blank Codex model uses the hcom default and is presented as default', () => {
  assert.equal(initialLaunchForm('codex').model, '')
  const component = readFileSync(new URL('../src/features/launch/LaunchAgent.tsx', import.meta.url), 'utf8')
  assert.match(component, /placeholder="default"/)
})

test('launch refusal keeps the server detail visible verbatim', () => {
  assert.equal(
    launchRefusal({ error: 'launch refused', detail: 'fleet spawn: branch already exists\nchoose another branch' }),
    'fleet spawn: branch already exists\nchoose another branch',
  )
})

test('launch confirmation offers the launched agent in the current space', () => {
  assert.deepEqual(launchConfirmation(['impl-vava']), {
    line: 'Launched impl-vava.',
    taskLine: 'It has no task yet — send it one from its panel.',
    action: { label: 'Open in this space', agent: 'impl-vava' },
  })
})

test('launch dialog traps Tab and Shift+Tab at its focus boundaries', () => {
  assert.equal(dialogTabTargetIndex(3, 4, false), 0)
  assert.equal(dialogTabTargetIndex(0, 4, true), 3)
  assert.equal(dialogTabTargetIndex(1, 4, false), null)
  assert.equal(dialogTabTargetIndex(2, 4, true), null)
  assert.equal(dialogTabTargetIndex(-1, 4, false), 0)
  assert.equal(dialogTabTargetIndex(-1, 0, false), null)
})

test('launch form explains that the default creates an isolated worktree', () => {
  assert.equal(initialLaunchForm().branch, '')
  assert.equal(initialLaunchForm().branchHelp, 'A fresh worktree is created for the agent.')
})
