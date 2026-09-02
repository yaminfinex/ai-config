import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  allNotesSignal,
  createNotesSelectorSignal,
  groupNotesSignal,
  notesCountSignal,
  notesGroupsSignal,
  notesStatusSignal,
  shallowEqualSnapshots,
} from '../src/features/notes/notesSubscription.ts'
import { createNotesStore } from '../src/features/notes/notesStore.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('fresh-array note snapshots render the rail and matching group only', () => {
  let id = 0
  let now = 0
  const storage = {
    length: 0,
    key: () => null,
    getItem: () => null,
    setItem: () => { throw new Error('storage unavailable') },
    removeItem: () => undefined,
  }
  const store = createNotesStore({
    storage,
    events: null,
    now: () => ++now,
    randomID: () => `id-${++id}`,
    schedule: () => 1,
    cancel: () => undefined,
  })
  const signals = {
    rail: allNotesSignal(store),
    agentA: groupNotesSignal(store, 'agent-a'),
    agentB: groupNotesSignal(store, 'agent-b'),
    selectedA: notesGroupsSignal(store, ['agent-a']),
    count: notesCountSignal(store),
    status: notesStatusSignal(store),
  }
  const renders = { rail: 0, agentA: 0, agentB: 0, selectedA: 0, count: 0, status: 0 }
  const dispose = Object.entries(signals).map(([surface, signal]) => signal.subscribe(() => { renders[surface as keyof typeof renders] += 1 }))

  const untouchedB = signals.agentB.getSnapshot()
  const added = store.add({ group: 'agent-a', text: 'keep this' })
  assert.equal(added.ok, true)
  assert.deepEqual(renders, { rail: 1, agentA: 1, agentB: 0, selectedA: 1, count: 1, status: 0 })
  assert.strictEqual(signals.agentB.getSnapshot(), untouchedB)

  assert.equal(store.flush(), false)
  assert.deepEqual(renders, { rail: 1, agentA: 1, agentB: 0, selectedA: 1, count: 1, status: 1 })

  renders.rail = 0; renders.agentA = 0; renders.agentB = 0; renders.selectedA = 0; renders.count = 0; renders.status = 0
  if (!added.ok) throw new Error('note setup failed')
  const edited = store.edit(added.value.id, { text: 'keep this updated' })
  assert.equal(edited.ok, true)
  assert.deepEqual(renders, { rail: 1, agentA: 1, agentB: 0, selectedA: 1, count: 0, status: 0 })

  renders.rail = 0; renders.agentA = 0; renders.agentB = 0; renders.selectedA = 0; renders.count = 0; renders.status = 0
  const moved = store.edit(added.value.id, { group: 'agent-b' })
  assert.equal(moved.ok, true)
  assert.deepEqual(renders, { rail: 1, agentA: 1, agentB: 1, selectedA: 1, count: 0, status: 0 })

  renders.rail = 0; renders.agentA = 0; renders.agentB = 0; renders.selectedA = 0; renders.count = 0; renders.status = 0
  const deleted = store.delete([added.value.id])
  assert.equal(deleted.ok, true)
  assert.deepEqual(renders, { rail: 1, agentA: 0, agentB: 1, selectedA: 0, count: 1, status: 0 })

  for (const stop of dispose) stop()
})

test('selector signals reconcile writes made before React subscribes', () => {
  let id = 0
  const store = createNotesStore({ storage: null, events: null, randomID: () => `id-${++id}` })
  const signal = groupNotesSignal(store, 'agent-a')

  const added = store.add({ group: 'agent-a', text: 'written before subscribe' })
  assert.equal(added.ok, true)
  assert.deepEqual(signal.getSnapshot(), [])

  const stop = signal.subscribe(() => undefined)
  assert.deepEqual(signal.getSnapshot().map((note) => note.text), ['written before subscribe'])
  stop()
})

test('selector snapshots stay referentially stable until their source notifies', () => {
  let current = ['one']
  const listeners = new Set<() => void>()
  const signal = createNotesSelectorSignal(
    (listener) => { listeners.add(listener); return () => listeners.delete(listener) },
    () => [...current],
    shallowEqualSnapshots,
  )
  const first = signal.getSnapshot()
  const stop = signal.subscribe(() => undefined)

  current = ['two']
  assert.strictEqual(signal.getSnapshot(), first)
  assert.strictEqual(signal.getSnapshot(), first)
  for (const listener of listeners) listener()
  const second = signal.getSnapshot()
  assert.notStrictEqual(second, first)
  assert.deepEqual(second, ['two'])
  assert.strictEqual(signal.getSnapshot(), second)
  stop()
})

test('notes consumers subscribe through cached external-store snapshots', () => {
  const provider = read('../src/features/notes/NotesProvider.tsx')
  assert.match(provider, /useSyncExternalStore/)
  assert.match(provider, /useMemo\(\(\) => allNotesSignal\(store\), \[store\]\)/)
  assert.match(provider, /useMemo\(\(\) => groupNotesSignal\(store, group\), \[group, store\]\)/)
  assert.match(provider, /useMemo\(\(\) => notesGroupsSignal\(store, JSON\.parse\(groupKey\) as string\[\]\), \[groupKey, store\]\)/)
  assert.match(provider, /useMemo\(\(\) => notesStatusSignal\(store\), \[store\]\)/)
  assert.match(provider, /useMemo\(\(\) => notesCountSignal\(store\), \[store\]\)/)
  assert.doesNotMatch(provider, /createNotesSelectorSignal/)
  assert.doesNotMatch(provider, /const \[notes, setNotes\]/)
})
