import assert from 'node:assert/strict'
import test from 'node:test'
import { appendComposerDraft, composerDraftKey, persistComposerDraft, readComposerDrafts, subscribeComposerDrafts } from '../src/composerState.ts'
import { draftCountLabel, draftMarks, draftTitle, sameDraftMarks } from '../src/features/drafts/draftMarksModel.ts'
import { attentionLabel, spaceAttention, totalAttention } from '../src/features/spaces/spaceAttentionModel.ts'

function memoryStorage() {
  const values = new Map<string, string>()
  return {
    get length() { return values.size },
    key: (index: number) => [...values.keys()][index] ?? null,
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
  }
}

test('a composer draft counts only with non-blank text; notes count by their agent group, never the unassigned one', () => {
  const marks = draftMarks({ mavu: 'half a thought', ziru: '   \n\t', kobe: '' }, [{ group: 'kobe' }, { group: 'kobe' }, { group: 'mavu' }, { group: 'general' }])
  assert.deepEqual(marks, { mavu: { message: true, notes: 1 }, kobe: { message: false, notes: 2 } })
  assert.deepEqual(draftMarks({}, []), {})
})

test('draft marks compare by who has what, not identity', () => {
  assert.ok(sameDraftMarks({ a: { message: true, notes: 0 } }, { a: { message: true, notes: 0 } }))
  assert.ok(!sameDraftMarks({ a: { message: true, notes: 0 } }, { a: { message: true, notes: 1 } }))
  assert.ok(!sameDraftMarks({ a: { message: true, notes: 0 } }, { b: { message: true, notes: 0 } }))
  assert.ok(!sameDraftMarks({}, { a: { message: false, notes: 1 } }))
})

test('an agent mark names the kinds it stands for', () => {
  assert.equal(draftTitle({ message: true, notes: 0 }), 'unsent message')
  assert.equal(draftTitle({ message: false, notes: 1 }), '1 unsent note')
  assert.equal(draftTitle({ message: true, notes: 3 }), 'unsent message, 3 unsent notes')
  assert.equal(draftCountLabel(1), '1 draft')
  assert.equal(draftCountLabel(2), '2 drafts')
})

test('a space rolls up its open agents with drafts beside unread and blocked, and the total counts each agent once', () => {
  const drafts = draftMarks({ mavu: 'x', away: 'y' }, [{ group: 'ziru' }])
  const one = spaceAttention(undefined, ['mavu', 'ziru', 'kobe'], {}, drafts)
  assert.deepEqual(one, { unread: [], blocked: [], drafts: ['mavu', 'ziru'] })
  const two = spaceAttention(undefined, ['mavu'], {}, drafts)
  assert.deepEqual(totalAttention([one, two]).drafts, ['mavu', 'ziru'])
})

test('labels add drafts to the waiting and blocked wording', () => {
  assert.equal(attentionLabel({ unread: [], blocked: [], drafts: ['a', 'b'] }), '2 drafts')
  assert.equal(attentionLabel({ unread: ['a'], blocked: [], drafts: ['b'] }), '1 agent waiting, 1 draft')
  assert.equal(attentionLabel({ unread: ['a'], blocked: ['c'], drafts: ['a', 'b'] }), '1 agent waiting, 1 blocked, 2 drafts')
  assert.equal(attentionLabel({ unread: [], blocked: [], drafts: [] }), 'no agents waiting')
})

test('composer drafts are read by agent across the store, and every local write signals a change', () => {
  const storage = memoryStorage()
  let changes = 0
  const unsubscribe = subscribeComposerDrafts(() => { changes++ })
  persistComposerDraft('agent/one', 'first', storage)
  persistComposerDraft('two', 'second', storage)
  storage.setItem('herder.web.other', 'ignored')
  assert.deepEqual(readComposerDrafts(storage), { 'agent/one': 'first', two: 'second' })
  persistComposerDraft('two', '', storage)
  assert.equal(storage.getItem(composerDraftKey('two')), null)
  assert.deepEqual(appendComposerDraft('agent/one', ['a note'], storage), { ok: true, text: 'first\n\na note' })
  assert.equal(changes, 4)
  unsubscribe()
  persistComposerDraft('two', 'again', storage)
  assert.equal(changes, 4)
  assert.deepEqual(readComposerDrafts(null), {})
})
