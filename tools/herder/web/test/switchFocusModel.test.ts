import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { arriveDecision, focusAtEnd, focusOrigin, type ArriveInput } from '../src/features/spaces/switchFocusModel.ts'

const ready = { disabled: false }
const decide = (input: Partial<ArriveInput>) => arriveDecision({ origin: 'other', activePanelKind: 'agent', composer: ready, panel: true, lastTry: false, ...input })

test('an arrival lands in the agent composer once it is drawn and writable', () => {
  assert.equal(decide({}), 'composer')
  assert.equal(decide({ origin: 'composer' }), 'composer', '⌥Tab from one composer lands in the next')
  assert.equal(decide({ composer: null }), 'wait', 'the panel has not mounted its composer yet')
  assert.equal(decide({ composer: null, panel: false }), 'wait', 'nor drawn its panel')
})

test('a read-only composer, or one that never mounts, hands the keys to the panel', () => {
  assert.equal(decide({ composer: { disabled: true } }), 'panel', 'a read-only viewer or retired agent')
  assert.equal(decide({ composer: null, lastTry: true }), 'panel', 'no composer by the last try')
  assert.equal(decide({ composer: null, panel: false, lastTry: true }), 'leave', 'nothing drawn to take focus')
})

test('focus stays put for a non-agent panel or an empty space', () => {
  assert.equal(decide({ activePanelKind: 'file' }), 'leave')
  assert.equal(decide({ activePanelKind: 'screen', composer: null }), 'leave', 'no waiting for a composer that never comes')
  assert.equal(decide({ activePanelKind: undefined, composer: null }), 'leave')
})

test('an arrival begun from a text field, such as a space rename, never steals its focus', () => {
  assert.equal(decide({ origin: 'text-field' }), 'leave')
  assert.equal(decide({ origin: 'text-field', composer: null }), 'leave')
})

test('a space chosen in ⌘K lands in its composer although the palette search had focus', () => {
  assert.equal(decide({ origin: 'quick-open' }), 'composer')
  assert.equal(decide({ origin: 'quick-open', composer: null }), 'wait')
  assert.equal(decide({ origin: 'quick-open', activePanelKind: 'file' }), 'leave', 'a non-agent panel keeps focus where it is')
  assert.equal(decide({ origin: 'quick-open', composer: { disabled: true } }), 'panel')
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
const arrive = readFileSync(new URL('../src/features/workspace/useArrive.ts', import.meta.url), 'utf8')
const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
const actions = readFileSync(new URL('../src/features/workspace/useWorkspaceActions.ts', import.meta.url), 'utf8')
const shortcuts = readFileSync(new URL('../src/features/workspace/useWorkspaceShortcuts.ts', import.meta.url), 'utf8')
const registry = readFileSync(new URL('../src/features/workspace/panelRegistry.tsx', import.meta.url), 'utf8')

test('in a zoom the rail, ⌥Tab and ⌘K switch through the focusing wrapper; ⇧⌥←/→ steps spaces', () => {
  assert.match(controller, /const switchSpaceFocusing = useSwitchSpaceFocusing\(switchSpace, readActiveSpaceID, arrive\)/)
  assert.match(controller, /const switchSpaceAnywhere = useCallback\(\(id: string, from\?: 'quick-open'\) => viewRef\.current\.kind === 'zoom' \? switchSpaceFocusing\(id, from\) : lens\.zoomCard\(id\)/)
  assert.match(controller, /useWorkspaceShortcuts\(\{[^}]*stepSpace: lens\.stepSpace, toggleRead, arrive \}\)/)
  assert.match(controller, /useSpaceSwitcher\(\{[^}]*switchSpace: switchSpaceAnywhere \}\)/)
  assert.match(controller, /switch: switchSpaceAnywhere,/)
})

test('the wrapper reads the origin before switching, then arrives', () => {
  assert.ok(hook.indexOf('focusOrigin(document.activeElement)') < hook.indexOf('switchSpace(spaceID)'), 'origin is captured before the old layout unmounts')
  assert.match(hook, /if \(!refocus && !switchSpace\(spaceID\)\) return false\n\s+arrive\(origin\)/)
})

test('one arrive path: it retries the decision until the arriving agent\'s own composer mounts and can take focus', () => {
  assert.match(arrive, /focusComposerWhenReady\(/)
  assert.match(arrive, /document\.querySelector\(alone === undefined \? '\.dv-active-group' : '\.alone-agent'\)/)
  assert.match(arrive, /candidate\.id === composerFieldId\(name\)/)
  assert.match(arrive, /activePanelKind: params\?\.kind/)
  assert.match(arrive, /if \(decision === 'wait'\) return null/)
  assert.match(arrive, /if \(!lastTry && !target\.checkVisibility\(\{ visibilityProperty: true \}\)\) return null/)
  assert.match(arrive, /decision === 'composer' \? focusAtEnd\(field!\) : target\.focus\(\{ preventScroll: true \}\)/)
})

test('every entry path arrives through it: zooms, alone, opens, tabs, ⌥←/→, back/forward and load', () => {
  assert.match(controller, /const arrive = useArrive\(apiRef, aloneAgent\)/)
  const zoomInto = controller.slice(controller.indexOf('const zoomInto'), controller.indexOf('const zoomOut'))
  assert.match(zoomInto, /if \(focus\) arrive\(\)/)
  const openAlone = controller.slice(controller.indexOf('const openAlone'), controller.indexOf('const inZoom'))
  assert.match(openAlone, /arrive\(\)/)
  assert.match(controller, /\{ switchSpace, applyRoute \},\n\s+\)\)\n\s+\}\n\s+arrive\(\)/, 'back/forward into a zoom or alone')
  assert.match(controller, /if \(viewRef\.current\.kind !== 'lens'\) arrive\(\)/, 'a page loaded on a zoom')
  assert.match(actions, /if \(focus\) arrive\(\)/)
  assert.match(shortcuts, /\?\.api\.setActive\(\)\n\s+arrive\(\)/, '⌥←/→')
  assert.match(registry, /onClick=\{\(event\) => \{ if \(!\(event\.target as Element\)\.closest\('\.dock-tab-close'\)\) actions\.arrive\(\) \}\}/, 'a tab click')
  assert.doesNotMatch(controller, /focusZoom/, 'no second focus path for zooms')
})

const quickOpen = readFileSync(new URL('../src/features/files/QuickOpen.tsx', import.meta.url), 'utf8')
const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')

test('⌘K switches with an explicit quick-open intent, refocuses the active space, and does not restore focus over the composer', () => {
  assert.match(app, /onSwitchSpace=\{workspace\.spaces\.switch\}/)
  assert.match(quickOpen, /chosen = onSwitchSpace\(row\.id, 'quick-open'\)\n\s+if \(chosen\) restoreFocus\.current = null/)
  assert.doesNotMatch(quickOpen, /row\.id === activeSpaceID \|\|/)
  assert.match(hook, /const origin = from \?\? focusOrigin\(document\.activeElement\)/)
  assert.match(hook, /const refocus = from === 'quick-open' && activeSpaceID\(\) === spaceID/)
  // The palette's search box is never the owner's text field, for a ⌘K agent row as for a space row.
  assert.match(arrive, /focusOrigin\(document\.activeElement\?\.closest\('\.quick-open'\) \? null : document\.activeElement\)/)
})
