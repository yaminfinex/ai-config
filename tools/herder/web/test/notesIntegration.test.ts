import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('rail and agent strip host exactly the same NotesList interaction surface', () => {
  const rail = read('../src/features/notes/NotesRail.tsx')
  const strip = read('../src/features/notes/AgentNotesStrip.tsx')
  const list = read('../src/features/notes/NotesList.tsx')
  assert.match(rail, /<NotesList/)
  assert.match(strip, /<NotesList/)
  assert.equal((`${rail}\n${strip}\n${list}`.match(/export function NotesList/g) ?? []).length, 1)
  assert.throws(() => read('../src/features/notes/NotesGroup.tsx'), /ENOENT/)
  assert.doesNotMatch(`${rail}\n${strip}`, /sendMessage|\/api\/agents/)
})

test('one selector component serves list assignment and capture targeting', () => {
  const list = read('../src/features/notes/NotesList.tsx')
  const capture = read('../src/features/notes/NoteCaptureChip.tsx')
  const selector = read('../src/features/notes/NotesSelector.tsx')
  assert.match(list, /<NotesSelector/)
  assert.match(capture, /<NotesSelector/)
  assert.equal((`${list}\n${capture}\n${selector}`.match(/export function NotesSelector/g) ?? []).length, 1)
})

test('note editing leaves copy, delete, enter, and A as native textarea input', () => {
  const list = read('../src/features/notes/NotesList.tsx')
  const start = list.indexOf('<textarea autoFocus')
  const end = list.indexOf('/> : <p>', start)
  const editor = list.slice(start, end)
  assert.ok(start >= 0 && end > start)
  assert.match(editor, /event\.key === 'Escape'/)
  assert.doesNotMatch(editor, /event\.key === 'Enter'|metaKey|ctrlKey|event\.key === 'Delete'|event\.key === 'Backspace'|toLowerCase\(\).*'a'/)
})

test('capture comment only claims its explicit escape and submit gestures', () => {
  const chip = read('../src/features/notes/NoteCaptureChip.tsx')
  const start = chip.indexOf('<textarea ref={commentRef}')
  const end = chip.indexOf('/>', start)
  const comment = chip.slice(start, end)
  assert.ok(start >= 0 && end > start)
  assert.match(comment, /event\.key === 'Escape'/)
  assert.match(comment, /event\.key === 'Enter'/)
  assert.doesNotMatch(comment, /metaKey|ctrlKey|event\.key === 'Delete'|event\.key === 'Backspace'|toLowerCase\(\).*'a'/)
})

test('list shortcuts yield to every native interactive control', () => {
  const list = read('../src/features/notes/NotesList.tsx')
  assert.match(list, /closest\('input, textarea, select, button, a\[href\], \[contenteditable="true"\], \.notes-selector'\)/)
  assert.match(list, /noteListAction\(event, editable\)/)
  assert.doesNotMatch(list, /noteListAction\(event, false\)/)
})

test('selector Enter leaves a focused option to its native button activation', () => {
  const selector = read('../src/features/notes/NotesSelector.tsx')
  assert.match(selector, /closest\('\[role="option"\]'\)/)
  assert.match(selector, /event\.key === 'Enter'.*!focusedOption/)
})

test('notes focus frames use the lifecycle-owned scheduler', () => {
  const files = [
    read('../src/features/notes/NotesList.tsx'),
    read('../src/features/notes/NoteQuickAdd.tsx'),
    read('../src/features/notes/NoteCaptureChip.tsx'),
    read('../src/features/notes/useNoteCapture.tsx'),
  ]
  for (const source of files) {
    assert.match(source, /useScheduledFrame/)
    assert.doesNotMatch(source, /window\.requestAnimationFrame/)
  }
  const lifecycle = read('../src/shared/lifecycle.ts')
  assert.match(lifecycle, /export function useScheduledFrame/)
  assert.match(lifecycle, /cancelAnimationFrame/)
})

test('agent notes remain directly above the one existing composer', () => {
  const panel = read('../src/features/transcript/AgentPanel.tsx')
  assert.match(panel, /<AgentNotesStrip[\s\S]*<Composer/)
  assert.equal((panel.match(/<Composer/g) ?? []).length, 1)
})

test('composer queue proves durable note storage before clearing the draft', () => {
  const panel = read('../src/features/transcript/AgentPanel.tsx')
  assert.match(panel, /queueComposerNote\(notesStore, name, text\)/)
  assert.doesNotMatch(panel, /notesStore\.add/)
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
  const list = read('../src/features/notes/NotesList.tsx')
  const rail = read('../src/features/notes/NotesRail.tsx')
  const handOff = read('../src/features/notes/noteHandOff.ts')
  assert.match(list, /handOffSelectedNotes/)
  assert.ok(handOff.indexOf('append(target, pending)') < handOff.indexOf('remove(notes.map'))
  assert.ok(handOff.indexOf('remove(notes.map') < handOff.indexOf('flush()'))
  assert.ok(rail.indexOf('appendComposerDraft') < rail.indexOf('onOpenAgent'))
})

test('the strip materializes only for notes and collapse is panel-lifetime state', () => {
  const strip = read('../src/features/notes/AgentNotesStrip.tsx')
  const rail = read('../src/features/notes/NotesRail.tsx')
  assert.match(strip, /useState\(true\)/)
  assert.match(strip, /if \(count === 0\) return null/)
  assert.doesNotMatch(strip, /readNotesStripCollapsed|persistNotesStripCollapsed|localStorage/)
  assert.match(strip, /<NoteQuickAdd group=\{agent\}/)
  assert.match(strip, /!collapsed && <NotesList/)
  assert.doesNotMatch(`${strip}\n${rail}`, /No notes|notes-empty|zero/i)
})

test('selector owns an internally scrolling list and keeps its highlight visible', () => {
  const selector = read('../src/features/notes/NotesSelector.tsx')
  const styles = read('../src/styles.css')
  assert.match(selector, /scrollIntoView\(\{ block: 'nearest' \}\)/)
  assert.match(selector, /selectorMove/)
  assert.match(selector, /inputRef\.current\?\.focus\(\)/)
  assert.match(styles, /\.notes-selector \[role='listbox'\][^}]*max-height: 192px;[^}]*overflow-y: auto;/)
})

test('capture allowlist markers are content-local, never panel-wide', () => {
  const agent = read('../src/features/transcript/TranscriptEntries.tsx')
  const panel = read('../src/features/transcript/AgentPanel.tsx')
  const file = read('../src/features/files/FilePanel.tsx')
  assert.match(agent, /data-note-capture-content/)
  assert.match(file, /data-note-capture-content/)
  assert.doesNotMatch(panel, /<main[^>]+data-note-capture-content/)
  assert.doesNotMatch(file, /<main[^>]+data-note-capture-content/)
})

test('notes storage families share one versioned namespace and stay disjoint from drafts and layout', () => {
  const store = read('../src/features/notes/notesStore.ts')
  assert.match(store, /notesStoragePrefix = 'herder\.web\.notes\.v1:'/)
  assert.match(store, /record:/)
  assert.match(store, /last-good:/)
  assert.match(store, /recovery:/)
  assert.doesNotMatch(store, /messageDraft|dockLayout/)
})
