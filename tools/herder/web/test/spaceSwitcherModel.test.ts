import assert from 'node:assert/strict'
import test from 'node:test'
import { highlightedSpace, idleSwitcher, reduceSwitcher, type SwitcherEvent, type SwitcherState } from '../src/features/spaces/spaceSwitcherModel.ts'
import { dwelledAgents, nextDwellDelay, nextViewing, viewDwellMs, viewedAgents } from '../src/features/spaces/viewingModel.ts'

const order = ['now', 'last', 'older', 'oldest']

function run(events: SwitcherEvent[], state: SwitcherState = idleSwitcher) {
  const commits: string[] = []
  for (const event of events) {
    const result = reduceSwitcher(state, event)
    state = result.state
    if (result.commit) commits.push(result.commit)
  }
  return { state, commits }
}

test('opening holds the list at once, highlighting the previous space forward and the oldest backward', () => {
  assert.deepEqual(run([{ type: 'cycle', direction: 'forward', order }]).state, { phase: 'holding', order, index: 1 })
  assert.equal(highlightedSpace(run([{ type: 'cycle', direction: 'backward', order }]).state), 'oldest')
})

test('a quick tap commits the last space', () => {
  const tap = run([{ type: 'cycle', direction: 'forward', order }, { type: 'release' }])
  assert.deepEqual(tap, { state: idleSwitcher, commits: ['last'] })
})

test('Tab and Shift-Tab wrap through the list; release commits the highlight', () => {
  const cycled = run([
    { type: 'cycle', direction: 'forward', order },
    { type: 'cycle', direction: 'forward', order }, { type: 'cycle', direction: 'forward', order },
    { type: 'cycle', direction: 'forward', order },
  ])
  assert.equal(highlightedSpace(cycled.state), 'now', 'wraps past the end')
  assert.equal(highlightedSpace(run([{ type: 'step', direction: 'backward' }], cycled.state).state), 'oldest')
  assert.deepEqual(run([{ type: 'step', direction: 'backward' }, { type: 'release' }], cycled.state).commits, ['oldest'])
})

test('releasing on the current space, Escape and blur all leave the space unchanged', () => {
  const back = run([{ type: 'cycle', direction: 'forward', order }, { type: 'step', direction: 'backward' }, { type: 'release' }])
  assert.deepEqual(back, { state: idleSwitcher, commits: [] })
  assert.deepEqual(run([{ type: 'cycle', direction: 'forward', order }, { type: 'cancel' }]), { state: idleSwitcher, commits: [] })
})

test('pointing at an option commits it, unless it is the current space or unknown', () => {
  const held = run([{ type: 'cycle', direction: 'forward', order }]).state
  assert.deepEqual(run([{ type: 'choose', id: 'oldest' }], held).commits, ['oldest'])
  assert.deepEqual(run([{ type: 'choose', id: 'now' }], held), { state: idleSwitcher, commits: [] })
  assert.deepEqual(run([{ type: 'choose', id: 'ghost' }], held).commits, [])
})

test('the switcher does not open for fewer than two spaces and ignores stray events while idle', () => {
  assert.deepEqual(run([{ type: 'cycle', direction: 'forward', order: ['only'] }]).state, idleSwitcher)
  assert.deepEqual(run([{ type: 'release' }, { type: 'step', direction: 'forward' }, { type: 'choose', id: 'now' }]), { state: idleSwitcher, commits: [] })
})

test('viewed agents are the active agent panels of visible groups while the page is visible', () => {
  const groups = [
    { visible: true, activeAgent: 'mavu' }, { visible: false, activeAgent: 'ziru' },
    { visible: true }, { visible: true, activeAgent: 'mavu' },
  ]
  assert.deepEqual(viewedAgents(groups, true), ['mavu'])
  assert.deepEqual(viewedAgents(groups, false), [])
})

test('a view only counts after the dwell, which survives unrelated layout churn', () => {
  assert.equal(viewDwellMs, 1000)
  const first = nextViewing({}, ['mavu'], 0)
  assert.deepEqual(first, { mavu: 0 })
  assert.equal(nextViewing(first, ['mavu'], 400), first, 'unchanged view keeps its start')
  const both = nextViewing(first, ['mavu', 'ziru'], 600)
  assert.deepEqual(both, { mavu: 0, ziru: 600 })
  assert.deepEqual(dwelledAgents(both, 999), [])
  assert.equal(nextDwellDelay(both, 999), 1)
  assert.deepEqual(dwelledAgents(both, 1000), ['mavu'])
  assert.equal(nextDwellDelay(both, 1000), 600)
  assert.deepEqual(dwelledAgents(both, 1600), ['mavu', 'ziru'])
  assert.equal(nextDwellDelay(both, 1600), null)
  const away = nextViewing(both, ['ziru'], 700)
  assert.deepEqual(away, { ziru: 600 })
  assert.deepEqual(nextViewing(away, ['mavu'], 900), { mavu: 900 }, 'tabbing back restarts the dwell')
})
