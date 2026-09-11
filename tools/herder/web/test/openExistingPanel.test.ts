import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

import { raiseExistingPanel } from '../src/features/workspace/openExistingModel.ts'

type Params = { kind: 'agent', name: string, preview: boolean }

function fake(active: boolean) {
  const log: string[] = []
  const panel = {
    params: { kind: 'agent', name: 'ziru', preview: false } as Params,
    api: { updateParameters: (next: Params) => log.push(`update ${JSON.stringify(next)}`), setActive: () => log.push('setActive') },
  }
  const merged = raiseExistingPanel(panel, 'agent:ziru', active ? 'agent:ziru' : 'agent:kolo', { kind: 'agent', name: 'ziru', preview: true }, {
    current: (raw) => raw as Params,
    merge: (current, next) => ({ ...next, preview: current.preview && next.preview }),
    onActiveParamsChanged: (next) => log.push(`history ${JSON.stringify(next)}`),
    invalidate: () => log.push('invalidate'),
    syncDock: () => log.push('sync'),
  })
  return { merged, log }
}

test('re-opening the ACTIVE panel never calls setActive (dockview would detach and re-append the content, resetting the transcript scroll); params merge, history, invalidate and sync still run', () => {
  const { merged, log } = fake(true)
  assert.deepEqual(merged, { kind: 'agent', name: 'ziru', preview: false })
  assert.deepEqual(log, ['update {"kind":"agent","name":"ziru","preview":false}', 'history {"kind":"agent","name":"ziru","preview":false}', 'invalidate', 'sync'])
  assert.equal(log.includes('setActive'), false)
})

test('re-opening an INACTIVE open panel activates it exactly once and does not touch history', () => {
  const { log } = fake(false)
  assert.deepEqual(log, ['update {"kind":"agent","name":"ziru","preview":false}', 'setActive', 'invalidate', 'sync'])
})

test('openPanel routes every existing-panel open through raiseExistingPanel and has no setActive of its own on that path', () => {
  const actions = readFileSync(new URL('../src/features/workspace/useWorkspaceActions.ts', import.meta.url), 'utf8')
  const branch = actions.slice(actions.indexOf("if (target.kind === 'existing') {"), actions.indexOf("return 'existing' as const"))
  assert.match(branch, /raiseExistingPanel\(target\.panel, id, api\.activePanel\?\.id, params, \{/)
  assert.doesNotMatch(branch, /setActive\(\)/)
})
