import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import test from 'node:test'
import {
  appendComposerDraft,
  focusComposerWhenReady,
  composerFieldId,
  composerDraftKey,
  blurComposerOnEscape,
  composerArrowUpAction,
  isComposerQueueShortcut,
  isComposerSendShortcut,
  persistComposerDraft,
  readComposerDraft,
  resizeComposerFromMirror,
  resolveComposerStorage,
  subscribeComposerDraft,
} from '../src/composerState.ts'

function memoryStorage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
  }
}

test('draft keys are versioned and isolated per agent', () => {
  const storage = memoryStorage()
  persistComposerDraft('agent/one', 'first draft', storage)
  persistComposerDraft('agent two', 'second draft', storage)
  assert.equal(readComposerDraft('agent/one', storage), 'first draft')
  assert.equal(readComposerDraft('agent two', storage), 'second draft')
  assert.notEqual(composerDraftKey('agent/one'), composerDraftKey('agent two'))
  persistComposerDraft('agent/one', '', storage)
  assert.equal(readComposerDraft('agent/one', storage), '')
  assert.equal(readComposerDraft('agent two', storage), 'second draft')
})

test('blocked browser storage degrades to a working non-persistent composer', () => {
  const blocked = resolveComposerStorage(() => {
    const error = new Error('Access is denied for this document.')
    error.name = 'SecurityError'
    throw error
  })

  assert.equal(blocked, null)
  assert.equal(readComposerDraft('agent/blocked', blocked), '')
  assert.doesNotThrow(() => persistComposerDraft('agent/blocked', 'usable draft', blocked))
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: true, metaKey: false }), true)
})

test('note hand-off appends with blank lines and notifies a mounted composer after persistence', () => {
  const storage = memoryStorage()
  persistComposerDraft('agent/one', 'existing draft', storage)
  const received: string[] = []
  const unsubscribe = subscribeComposerDraft('agent/one', (text) => received.push(text))
  const result = appendComposerDraft('agent/one', ['first note', 'second note'], storage)
  unsubscribe()

  assert.deepEqual(result, { ok: true, text: 'existing draft\n\nfirst note\n\nsecond note' })
  assert.equal(readComposerDraft('agent/one', storage), result.text)
  assert.deepEqual(received, [result.text])
})

test('failed note hand-off leaves the composer draft and notes available to retry', () => {
  const blocked = {
    getItem: () => 'existing draft',
    setItem: () => { throw new Error('quota') },
    removeItem: () => undefined,
  }
  assert.deepEqual(appendComposerDraft('agent/blocked', ['kept note'], blocked), {
    ok: false,
    reason: 'The composer draft could not be saved. Your notes were left in place.',
  })
})

test('only Ctrl+Enter and Cmd+Enter are send shortcuts', () => {
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: true, metaKey: false }), true)
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: false, metaKey: true }), true)
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: true, metaKey: false, shiftKey: true }), false)
  assert.equal(isComposerSendShortcut({ key: 'Enter', ctrlKey: false, metaKey: false }), false)
  assert.equal(isComposerSendShortcut({ key: 'a', ctrlKey: true, metaKey: false }), false)
})

test('only physical Option+Enter queues a note without stealing send or newline keys', () => {
  assert.equal(isComposerQueueShortcut({ key: 'Dead', code: 'Enter', altKey: true, ctrlKey: false, metaKey: false, shiftKey: false }), true)
  assert.equal(isComposerQueueShortcut({ key: 'Enter', code: 'Enter', altKey: true, ctrlKey: false, metaKey: false, shiftKey: false }), true)
  assert.equal(isComposerQueueShortcut({ key: 'Enter', code: 'Enter', altKey: true, ctrlKey: false, metaKey: true, shiftKey: false }), false)
  assert.equal(isComposerQueueShortcut({ key: 'Enter', code: 'Enter', altKey: false, ctrlKey: false, metaKey: false, shiftKey: false }), false)
  assert.equal(isComposerQueueShortcut({ key: 'Enter', code: 'Enter', altKey: true, ctrlKey: false, metaKey: false, shiftKey: true }), false)
})

test('each open agent composer gets a unique DOM id', () => {
  assert.equal(composerFieldId('agent one'), 'message-agent%20one')
  assert.notEqual(composerFieldId('agent one'), composerFieldId('agent two'))
})

test('Escape blurs the composer without claiming the event', () => {
  let blurred = false
  assert.equal(blurComposerOnEscape({ key: 'Escape', currentTarget: { blur: () => { blurred = true } } }), true)
  assert.equal(blurred, true)
  blurred = false
  assert.equal(blurComposerOnEscape({ key: 'Enter', currentTarget: { blur: () => { blurred = true } } }), false)
  assert.equal(blurred, false)
})

test('send success refetches the transcript immediately, not just agent status', () => {
  const composer = readFileSync(new URL('../src/features/composer/Composer.tsx', import.meta.url), 'utf8')
  assert.match(composer, /mutationFn: \(text: string\) => sendWithRefresh\(queryClient, name, text\)/)
  const shared = readFileSync(new URL('../src/sendRefresh.ts', import.meta.url), 'utf8')
  const success = shared.slice(shared.indexOf('export async function sendWithRefresh'), shared.indexOf('} catch', shared.indexOf('export async function sendWithRefresh')))
  assert.match(success, /queryKeys\.agent\(agent\)/)
  assert.match(success, /queryKeys\.entries\(agent\)/)
  assert.match(success, /settleSendRefresh\(token, true, refresh\)/)
})

test('composer queue clears only after the notes persistence path proves success', () => {
  const composer = readFileSync(new URL('../src/features/composer/Composer.tsx', import.meta.url), 'utf8')
  const queue = composer.slice(composer.indexOf('if (isComposerQueueShortcut'), composer.indexOf('if (!isComposerSendShortcut'))
  assert.match(queue, /const queued = onQueue\(message\)/)
  assert.ok(queue.indexOf('if (!queued.ok)') < queue.indexOf("persistComposerDraft(name, '')"))
  assert.ok(queue.indexOf("persistComposerDraft(name, '')") < queue.indexOf("setMessage('')"))
  assert.match(queue, /setSendProblem\(queued\.reason\)/)
  const panel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  assert.match(panel, /queueComposerNote\(notesStore, name, text\)/)
  assert.doesNotMatch(panel, /notesStore\.add/)
})

test('focusComposerWhenReady retries until the composer mounts, then stops quietly', () => {
  let focused = 0
  const pending: Array<() => void> = []
  const schedule = (callback: () => void) => { pending.push(callback) }
  let composer: { focus: () => void } | null = null
  focusComposerWhenReady(() => composer, schedule, 5)
  assert.equal(focused, 0)
  assert.equal(pending.length, 1)
  pending.shift()?.()
  composer = { focus: () => { focused += 1 } }
  pending.shift()?.()
  assert.equal(focused, 1)
  assert.equal(pending.length, 0)

  composer = null
  focusComposerWhenReady(() => composer, schedule, 2)
  pending.shift()?.()
  pending.shift()?.()
  assert.equal(pending.length, 0)
  assert.equal(focused, 1)
})

test('focusComposerWhenReady cancels its outstanding frame', () => {
  const callbacks = new Map<number, () => void>()
  const cancelled: number[] = []
  let nextHandle = 1
  const cancel = focusComposerWhenReady(
    () => null,
    (callback) => {
      const handle = nextHandle++
      callbacks.set(handle, callback)
      return handle
    },
    20,
    (handle) => {
      cancelled.push(handle)
      callbacks.delete(handle)
    },
  )
  cancel()
  assert.deepEqual(cancelled, [1])
  assert.equal(callbacks.size, 0)
})

test('selecting an agent focuses its composer only on user-driven opens', () => {
  const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
  const actions = readFileSync(new URL('../src/features/workspace/useWorkspaceActions.ts', import.meta.url), 'utf8')
  const registry = readFileSync(new URL('../src/features/workspace/panelRegistry.tsx', import.meta.url), 'utf8')
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  assert.match(app, /onPreviewAgent=\{\(name, placement\) => openAgent\(name, true, placement, true\)\}/)
  assert.match(app, /onPinAgent=\{\(name, placement\) => openAgent\(name, false, placement, true\)\}/)
  assert.match(registry, /onOpenAgent=\{\(name, placement\) => workspace\.openAgent\(name, true, placementInGroup\(placement, api\.group\.id\), true\)\}/)
  const applyRoute = controller.slice(controller.indexOf('const applyRoute'), controller.indexOf('const onDockReady'))
  assert.match(applyRoute, /openPanel\(\{ \.\.\.route\.params, preview: true \}, undefined, false\)/)
  assert.doesNotMatch(applyRoute, /openPanel\([^\n]*true\)/)
  assert.match(actions, /focusComposerWhenReady/)
})

test('deletion and mid-draft measurement never collapse the live composer', () => {
  const heightWrites: string[] = []
  let height = '127px'
  const composer = {
    offsetWidth: 420,
    style: {
      get height() { return height },
      set height(value: string) { height = value; heightWrites.push(value) },
      overflowY: 'hidden',
    },
  }
  const mirror = { scrollHeight: 52, style: { width: '' } }

  resizeComposerFromMirror(composer, mirror)
  assert.deepEqual(heightWrites, ['52px'])
  assert.equal(mirror.style.width, '420px')

  mirror.scrollHeight = 52 // A same-height mid-draft edit.
  resizeComposerFromMirror(composer, mirror)
  assert.deepEqual(heightWrites, ['52px', '52px'])
  assert.ok(!heightWrites.includes('0px'))

  const source = readFileSync(new URL('../src/features/composer/Composer.tsx', import.meta.url), 'utf8')
  assert.match(source, /resizeComposerFromMirror\(composer, mirror\)/)
  assert.match(source, /aria-hidden="true"[\s\S]*className="composer-measure"[\s\S]*inert/)
  assert.doesNotMatch(source, /composer\.style\.height = '0px'/)
})

test('mirror measurement preserves every TASK-82 composer scenario', () => {
  const scenarios = [
    { name: 'growth under threshold', scrollHeight: 71, height: '71px', overflowY: 'hidden' },
    { name: 'paste-replace shrink', scrollHeight: 52, height: '52px', overflowY: 'hidden' },
    { name: 'single-shot clear', scrollHeight: 34, height: '34px', overflowY: 'hidden' },
    { name: '160px ceiling', scrollHeight: 278, height: '160px', overflowY: 'auto' },
    { name: 'mount restore', scrollHeight: 108, height: '108px', overflowY: 'hidden' },
  ]

  for (const scenario of scenarios) {
    const composer = { offsetWidth: 420, style: { height: '34px', overflowY: 'hidden' } }
    const mirror = { scrollHeight: scenario.scrollHeight, style: { width: '' } }
    resizeComposerFromMirror(composer, mirror)
    assert.equal(composer.style.height, scenario.height, scenario.name)
    assert.equal(composer.style.overflowY, scenario.overflowY, scenario.name)
  }

  const css = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')
  const mirrorRule = css.match(/\.send-box textarea\.composer-measure \{([^}]*)\}/)?.[1] ?? ''
  assert.match(mirrorRule, /position: absolute/)
  assert.doesNotMatch(mirrorRule, /min-height: 0/)
})

test('ArrowUp moves into the notes list only from the start of the prompt with notes present and no modifiers', () => {
  const base = { key: 'ArrowUp', altKey: false, ctrlKey: false, metaKey: false, shiftKey: false, value: '', selectionStart: 0, selectionEnd: 0, hasNotes: true }
  assert.equal(composerArrowUpAction(base), 'notes')
  assert.equal(composerArrowUpAction({ ...base, value: 'draft' }), 'notes')
  assert.equal(composerArrowUpAction({ ...base, value: 'draft', selectionStart: 2, selectionEnd: 2 }), null)
  assert.equal(composerArrowUpAction({ ...base, value: 'draft', selectionStart: 0, selectionEnd: 3 }), null)
  assert.equal(composerArrowUpAction({ ...base, value: 'draft', selectionStart: 3, selectionEnd: 0 }), 'notes')
  assert.equal(composerArrowUpAction({ ...base, hasNotes: false }), null)
  assert.equal(composerArrowUpAction({ ...base, shiftKey: true }), null)
  assert.equal(composerArrowUpAction({ ...base, metaKey: true }), null)
  assert.equal(composerArrowUpAction({ ...base, isComposing: true }), null)
  assert.equal(composerArrowUpAction({ ...base, key: 'ArrowDown' }), null)
})

test('ArrowUp travels as props: the composer calls back, the panel counts a request, the strip un-collapses, the list lands', () => {
  const composer = readFileSync(new URL('../src/features/composer/Composer.tsx', import.meta.url), 'utf8')
  assert.match(composer, /composerArrowUpAction\(\{[^}]*hasNotes \}\) === 'notes'[\s\S]*?event\.preventDefault\(\)\n\s*onNotesFocus\(\)/)
  assert.match(composer, /onNotesFocus: \(\) => void\n/)
  const panel = readFileSync(new URL('../src/features/transcript/AgentPanel.tsx', import.meta.url), 'utf8')
  assert.match(panel, /onNotesFocus=\{\(\) => setNotesFocusRequest\(\(current\) => current \+ 1\)\}/)
  assert.match(panel, /<AgentNotesStrip [^>]*focusRequest=\{notesFocusRequest\}/)
  const strip = readFileSync(new URL('../src/features/notes/AgentNotesStrip.tsx', import.meta.url), 'utf8')
  assert.match(strip, /if \(!focusRequest\) return\n\s*setCollapsed\(false\)\n\s*setExpandedBy\(focusRequest\)\n\s*\}, \[focusRequest\]\)/)
  assert.match(strip, /focusRequest: number \}/)
  assert.match(strip, /useEffect\(\(\) => \{ if \(expandedBy\) setExpandedBy\(0\) \}, \[expandedBy\]\)/)
  assert.match(strip, /onClick=\{\(\) => setCollapsed\(\(current\) => !current\)\}/)
  assert.equal(strip.match(/setExpandedBy\(0\)/g)?.length, 1)
  assert.match(strip, /<NotesList [^>]*focusRequest=\{expandedBy\} returnTo=\{agent\}/)
  const list = readFileSync(new URL('../src/features/notes/NotesList.tsx', import.meta.url), 'utf8')
  assert.match(list, /if \(!focusRequest \|\| !returnTo\) return\n\s*const landing = notesFocusLanding\(selection, notes, returnTo\)[\s\S]*?setSelection\(selectionAfterClick\(selection, ids, landing, \{ shift: false, command: false \}\)\)\n\s*focus\(landing\)[\s\S]*?\}, \[focusRequest\]\)/)
  const actions = readFileSync(new URL('../src/features/workspace/useWorkspaceActions.ts', import.meta.url), 'utf8')
  assert.match(actions, /field\.disabled \? field\.closest<HTMLElement>\('\.agent-page'\)/)
})

test('the notes focus window event no longer exists anywhere in src', () => {
  const root = new URL('../src/', import.meta.url)
  const walk = (dir: URL): string[] => readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const child = new URL(entry.name + (entry.isDirectory() ? '/' : ''), dir)
    return entry.isDirectory() ? walk(child) : [readFileSync(child, 'utf8')]
  })
  for (const source of walk(root)) {
    assert.doesNotMatch(source, /herder:notes-focus|notesFocusEvent|NotesFocusDetail/)
  }
})

test('the notes rail never reacts to a focus request: no window listener in the list, no request props from the rail', () => {
  const list = readFileSync(new URL('../src/features/notes/NotesList.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(list, /useDOMEvent\(window|useDOMEvent<[^>]*>\(window/)
  const rail = readFileSync(new URL('../src/features/notes/NotesRail.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(rail, /focusRequest|returnTo/)
  const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
  assert.doesNotMatch(controller, /notesFocusEvent/)
  assert.match(controller, /const notesFocusReturn = useRef<HTMLElement \| null>\(null\)/)
})

test('Escape with nothing selected in the strip list returns to that agent composer', () => {
  const list = readFileSync(new URL('../src/features/notes/NotesList.tsx', import.meta.url), 'utf8')
  assert.match(list, /action === 'clear' && selection\.selected\.size === 0 && returnTo\) \{\n\s*document\.getElementById\(composerFieldId\(returnTo\)\)\?\.focus\(\)/)
  const strip = readFileSync(new URL('../src/features/notes/AgentNotesStrip.tsx', import.meta.url), 'utf8')
  assert.match(strip, /returnTo=\{agent\}/)
})
