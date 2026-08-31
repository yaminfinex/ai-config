import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('rail and agent strip host the same NotesGroup interaction surface', () => {
  const rail = read('../src/features/notes/NotesRail.tsx')
  const strip = read('../src/features/notes/AgentNotesStrip.tsx')
  assert.match(rail, /<NotesGroup/)
  assert.match(strip, /<NotesGroup/)
  assert.doesNotMatch(`${rail}\n${strip}`, /sendMessage|\/api\/agents/)
})

test('agent notes remain directly above the one existing composer', () => {
  const panel = read('../src/features/transcript/AgentPanel.tsx')
  assert.match(panel, /<AgentNotesStrip[\s\S]*<Composer/)
  assert.equal((panel.match(/<Composer/g) ?? []).length, 1)
})

test('capture is shared by file, diff, and transcript panels and never opens the rail', () => {
  const file = read('../src/features/files/FilePanel.tsx')
  const agent = read('../src/features/transcript/AgentPanel.tsx')
  const capture = read('../src/features/notes/useNoteCapture.tsx')
  assert.match(file, /kind: 'diff'/)
  assert.match(file, /kind: 'file'/)
  assert.match(agent, /kind: 'transcript'/)
  assert.match(capture, /store\.add/)
  assert.doesNotMatch(capture, /toggleNotesRail|setNotesRail|open.*rail/i)
})

test('hand-off persists the composer append before deleting selected notes', () => {
  const group = read('../src/features/notes/NotesGroup.tsx')
  const rail = read('../src/features/notes/NotesRail.tsx')
  const handOff = read('../src/features/notes/noteHandOff.ts')
  assert.match(group, /handOffSelectedNotes/)
  assert.ok(handOff.indexOf('append(target, pending)') < handOff.indexOf('remove(notes.map'))
  assert.ok(handOff.indexOf('remove(notes.map') < handOff.indexOf('flush()'))
  assert.ok(rail.indexOf('appendComposerDraft') < rail.indexOf('onOpenAgent'))
})

test('notes storage families share one versioned namespace and stay disjoint from drafts and layout', () => {
  const store = read('../src/features/notes/notesStore.ts')
  assert.match(store, /notesStoragePrefix = 'herder\.web\.notes\.v1:'/)
  assert.match(store, /record:/)
  assert.match(store, /last-good:/)
  assert.match(store, /recovery:/)
  assert.doesNotMatch(store, /messageDraft|dockLayout/)
})
