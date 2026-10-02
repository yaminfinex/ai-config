import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { focusAtEnd, focusOrigin, switchFocusDecision, type SwitchFocusInput } from '../src/features/spaces/switchFocusModel.ts'

const ready = { disabled: false, visible: true }
const decide = (input: Partial<SwitchFocusInput>) => switchFocusDecision({ origin: 'other', activePanelKind: 'agent', composer: ready, ...input })

test('a switch lands in the active agent composer once it is visible and enabled', () => {
  assert.equal(decide({}), 'focus')
  assert.equal(decide({ origin: 'composer' }), 'focus', '⌥Tab from one composer lands in the next')
  assert.equal(decide({ composer: null }), 'wait', 'the restored panel has not mounted its composer yet')
})

test('focus stays put for a non-agent panel, a read-only or hidden composer, or an empty space', () => {
  assert.equal(decide({ activePanelKind: 'file' }), 'leave')
  assert.equal(decide({ activePanelKind: 'screen', composer: null }), 'leave', 'no waiting for a composer that never comes')
  assert.equal(decide({ activePanelKind: undefined, composer: null }), 'leave')
  assert.equal(decide({ composer: { disabled: true, visible: true } }), 'leave')
  assert.equal(decide({ composer: { disabled: false, visible: false } }), 'leave')
})

test('a switch begun from a text field, such as a space rename, never steals its focus', () => {
  assert.equal(decide({ origin: 'text-field' }), 'leave')
  assert.equal(decide({ origin: 'text-field', composer: null }), 'leave')
})

test('the origin tells a composer from other text fields and from buttons', () => {
  // Answers the two queries focusOrigin makes by tag, type and data-composer.
  const element = (tag: string, { type = '', composer = false } = {}) => ({
    matches: (query: string) => query === 'textarea[data-composer]'
      ? tag === 'textarea' && composer
      : tag === 'textarea' || tag === 'select' || (tag === 'input' && !query.includes(`[type=${type}]`)),
  })
  assert.equal(focusOrigin(null), 'other')
  assert.equal(focusOrigin(element('textarea', { composer: true })), 'composer')
  assert.equal(focusOrigin(element('input', { type: 'text' })), 'text-field', 'a space rename')
  assert.equal(focusOrigin(element('input', { type: 'checkbox' })), 'other')
  assert.equal(focusOrigin(element('textarea')), 'text-field', 'a note')
  assert.equal(focusOrigin(element('button')), 'other', 'a rail row')
})

test('the caret goes after the draft and the field scrolls to it', () => {
  const calls: unknown[] = []
  const field = {
    value: 'draft\nsecond line', scrollTop: 0, scrollHeight: 240,
    focus: (options: unknown) => calls.push(['focus', options]),
    setSelectionRange: (start: number, end: number) => calls.push(['select', start, end]),
  }
  focusAtEnd(field as unknown as HTMLTextAreaElement)
  assert.deepEqual(calls, [['focus', { preventScroll: true }], ['select', 17, 17]])
  assert.equal(field.scrollTop, 240)
})

const hook = readFileSync(new URL('../src/features/workspace/useSwitchSpaceFocusing.ts', import.meta.url), 'utf8')
const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')

test('rail, ⌥Tab switcher and ⇧⌥←/→ all switch through the focusing wrapper', () => {
  assert.match(controller, /const switchSpaceFocusing = useSwitchSpaceFocusing\(apiRef, switchSpace\)/)
  assert.match(controller, /useWorkspaceShortcuts\(\{[^}]*switchSpace: switchSpaceFocusing, toggleRead \}\)/)
  assert.match(controller, /useSpaceSwitcher\(\{[^}]*switchSpace: switchSpaceFocusing \}\)/)
  assert.match(controller, /switch: switchSpaceFocusing,/)
})

test('the wrapper reads the origin before switching and retries the decision until the composer mounts', () => {
  assert.ok(hook.indexOf('focusOrigin(document.activeElement)') < hook.indexOf('switchSpace(spaceID)'), 'origin is captured before the old layout unmounts')
  assert.match(hook, /if \(!switchSpace\(spaceID\)\) return false/)
  assert.match(hook, /document\.querySelector<HTMLTextAreaElement>\('\.dv-active-group textarea\[data-composer\]'\)/)
  assert.match(hook, /activePanelKind: panelParams\(api\?\.activePanel\?\.params\)\?\.kind/)
  assert.match(hook, /if \(decision === 'wait'\) return null/)
  assert.match(hook, /if \(decision === 'focus' && field\) focusAtEnd\(field\)/)
  assert.match(hook, /focusComposerWhenReady\(/)
})
