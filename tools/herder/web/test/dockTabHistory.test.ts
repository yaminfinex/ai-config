import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { preserveDockTabBrowserHistory } from '../src/features/workspace/dockTabHistoryModel.ts'

function keyEvent(key: string, modifiers: { metaKey?: boolean, ctrlKey?: boolean }, inTab = true) {
  let stopped = false
  return {
    event: {
      key,
      metaKey: modifiers.metaKey ?? false,
      ctrlKey: modifiers.ctrlKey ?? false,
      target: { closest: (selector: string) => inTab && selector === '.dv-tab' ? {} : null } as unknown as EventTarget,
      stopPropagation: () => { stopped = true },
    },
    stopped: () => stopped,
  }
}

test('dock tabs stop only modified horizontal arrows before Dockview can prevent the browser default', () => {
  for (const [key, modifiers, inTab, expected] of [
    ['ArrowLeft', { metaKey: true }, true, true],
    ['ArrowRight', { ctrlKey: true }, true, true],
    ['ArrowUp', { metaKey: true }, true, false],
    ['ArrowLeft', {}, true, false],
    ['ArrowLeft', { metaKey: true }, false, false],
  ] as const) {
    const probe = keyEvent(key, modifiers, inTab)
    preserveDockTabBrowserHistory(probe.event)
    assert.equal(probe.stopped(), expected)
  }
})

test('the dock-root listener is capture-phase and never prevents browser navigation', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const model = readFileSync(new URL('../src/features/workspace/dockTabHistoryModel.ts', import.meta.url), 'utf8')
  assert.match(app, /className="dock-host" onKeyDownCapture=\{preserveDockTabBrowserHistory\}/)
  assert.match(model, /stopPropagation\(\)/)
  assert.doesNotMatch(model, /preventDefault/)
})
