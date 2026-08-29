import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { subscribeDOMEvent } from '../src/shared/lifecycle.ts'
import { themeType } from '../src/shared/themeSignal.ts'

test('DOM subscriptions deliver through one disposer', () => {
  const target = new EventTarget()
  let deliveries = 0
  const dispose = subscribeDOMEvent(target, 'probe', () => { deliveries += 1 })
  target.dispatchEvent(new Event('probe'))
  dispose()
  target.dispatchEvent(new Event('probe'))
  assert.equal(deliveries, 1)
})

test('size observers use the shared lifecycle hook', () => {
  const follow = readFileSync(new URL('../src/shared/useFollowScroll.tsx', import.meta.url), 'utf8')
  const strip = readFileSync(new URL('../src/features/transcript/AgentContextStrip.tsx', import.meta.url), 'utf8')
  const screen = readFileSync(new URL('../src/features/screen/ScreenPanel.tsx', import.meta.url), 'utf8')
  for (const source of [follow, strip, screen]) {
    assert.match(source, /useSizeObserver\(/)
    assert.doesNotMatch(source, /new ResizeObserver/)
  }
})

test('the shared theme signal reads the document theme truth', () => {
  assert.equal(themeType({ dataset: { theme: 'light' } } as HTMLElement), 'light')
  assert.equal(themeType({ dataset: { theme: 'system' } } as HTMLElement), 'dark')
  const pierre = readFileSync(new URL('../src/features/git/PierreView.tsx', import.meta.url), 'utf8')
  assert.match(pierre, /useThemeType\(/)
  assert.doesNotMatch(pierre, /MutationObserver/)
})

test('every scheduled frame in the refactor surface has cancellation', () => {
  const quickOpen = readFileSync(new URL('../src/features/files/QuickOpen.tsx', import.meta.url), 'utf8')
  const pierre = readFileSync(new URL('../src/features/git/PierreView.tsx', import.meta.url), 'utf8')
  const composer = readFileSync(new URL('../src/composerState.ts', import.meta.url), 'utf8')
  assert.match(quickOpen, /cancelAnimationFrame/)
  assert.match(pierre, /cancelAnimationFrame/)
  assert.match(composer, /cancel/)
})
