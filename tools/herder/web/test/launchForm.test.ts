import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { changeLaunchTool, dialogTabTargetIndex, initialLaunchForm, launchConfirmation, launchModelLabel, launchRequest, launchRefusal } from '../src/features/launch/launchModel.ts'

test('launch form starts with plain defaults and curated models', () => {
  assert.deepEqual(initialLaunchForm(), {
    tool: 'claude',
    model: 'claude-opus-5-5',
    modelOptions: ['claude-opus-5-5', 'claude-fable-5-1'],
    effort: 'medium',
    effortOptions: ['low', 'medium', 'high', 'xhigh', 'max'],
    tag: 'impl',
  })
  assert.deepEqual(initialLaunchForm('codex'), {
    tool: 'codex',
    model: 'gpt-6-astra',
    modelOptions: ['gpt-6-astra'],
    effort: '',
    effortOptions: ['low', 'medium', 'high', 'xhigh'],
    tag: 'impl',
  })
})

test('launch request omits blank effort and serializes a selected effort', () => {
  const defaults = initialLaunchForm()
  assert.equal('effort' in launchRequest(initialLaunchForm('codex'), 'w1'), false)
  assert.equal('effort' in launchRequest({ ...defaults, effort: '  ' }, 'w1'), false)
  assert.deepEqual(launchRequest(defaults, 'w1'), {
    tool: 'claude',
    model: 'claude-opus-5-5',
    effort: 'medium',
    tag: 'impl',
    workspace: 'w1',
  })
  assert.deepEqual(launchRequest({ ...defaults, effort: ' high ' }, 'w1'), {
    tool: 'claude',
    model: 'claude-opus-5-5',
    effort: 'high',
    tag: 'impl',
    workspace: 'w1',
  })
})

test('switching tool resets model and effort to that tool defaults', () => {
  const claude = initialLaunchForm()
  const codex = changeLaunchTool({ ...claude, model: 'custom', effort: 'max', tag: 'rev' }, 'codex')
  assert.deepEqual(codex, { ...initialLaunchForm('codex'), tag: 'rev' })
  assert.deepEqual(changeLaunchTool({ ...codex, effort: 'high' }, 'claude'), { ...claude, tag: 'rev' })
})

test('model suggestions present Opus 5.5 before Fable 5.1, and GPT-6 Astra for Codex', () => {
  assert.deepEqual(initialLaunchForm('claude').modelOptions.map(launchModelLabel), ['Opus 5.5', 'Fable 5.1'])
  assert.deepEqual(initialLaunchForm('codex').modelOptions.map(launchModelLabel), ['GPT-6 Astra'])
})

test('a cleared model is presented as the hcom default', () => {
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
  assert.deepEqual(launchConfirmation(['impl-vava'], 'repo', 'w1:p9'), {
    line: 'Launched impl-vava in repo · pane w1:p9.',
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
