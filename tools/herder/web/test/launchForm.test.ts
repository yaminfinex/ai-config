import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { changeLaunchTool, dialogTabTargetIndex, initialLaunchForm, launchConfirmation, launchModelLabel, launchRequest, launchRefusal } from '../src/features/launch/launchModel.ts'

test('launch form starts with plain defaults and curated models', () => {
  assert.deepEqual(initialLaunchForm(), {
    tool: 'claude',
    model: 'claude-fable-5-1',
    modelOptions: ['claude-fable-5-1', 'opus', 'sonnet'],
    effort: '',
    effortOptions: ['low', 'medium', 'high', 'xhigh', 'max'],
    tag: 'impl',
    repo: '',
    branch: '',
    branchHelp: 'A fresh worktree is created for the agent.',
  })
  assert.deepEqual(initialLaunchForm('codex'), {
    tool: 'codex',
    model: '',
    modelOptions: ['gpt-5.4', 'gpt-5.4-mini'],
    effort: '',
    effortOptions: ['low', 'medium', 'high', 'xhigh'],
    tag: 'impl',
    repo: '',
    branch: '',
    branchHelp: 'A fresh worktree is created for the agent.',
  })
})

test('launch request omits blank effort and serializes a selected effort', () => {
  const defaults = initialLaunchForm()
  assert.equal('effort' in launchRequest(defaults), false)
  assert.deepEqual(launchRequest({ ...defaults, effort: ' high ' }), {
    tool: 'claude',
    model: 'claude-fable-5-1',
    effort: 'high',
    tag: 'impl',
    repo: '',
    branch: '',
  })
  assert.equal(changeLaunchTool({ ...defaults, effort: 'max' }, 'codex').effort, '')
})

test('Claude model suggestions present Fable 5.1 before Opus and Sonnet', () => {
  const form = initialLaunchForm('claude')
  assert.deepEqual(form.modelOptions.map(launchModelLabel), ['Fable 5.1', 'Opus', 'Sonnet'])
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
  assert.equal(dialogTabTargetIndex(4, 5, false), 0)
  assert.equal(dialogTabTargetIndex(0, 5, true), 4)
  assert.equal(dialogTabTargetIndex(2, 5, false), null)
  assert.equal(dialogTabTargetIndex(3, 5, true), null)
  assert.equal(dialogTabTargetIndex(-1, 5, false), 0)
  assert.equal(dialogTabTargetIndex(-1, 0, false), null)
})

test('launch form explains that the default creates an isolated worktree', () => {
  assert.equal(initialLaunchForm().branch, '')
  assert.equal(initialLaunchForm().branchHelp, 'A fresh worktree is created for the agent.')
})
