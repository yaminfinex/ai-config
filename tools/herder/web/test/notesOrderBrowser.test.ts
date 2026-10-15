import assert from 'node:assert/strict'
import test from 'node:test'

import { fixtureAPI, startBrowser } from './browserFixture.ts'

// Enough older unassigned notes to overflow the notes rail.
const seeded = Array.from({ length: 30 }, (_, index) => ({
  version: 1, writeID: `seed-write-${index}`,
  record: { id: `seed-${String(index).padStart(2, '0')}`, group: 'general', text: `fixture note ${index}`, created: 1_000_000 + index, updated: 1_000_000 + index },
}))

test('a new note lands at the bottom of the rail and is scrolled into view; an edit keeps its place', { timeout: 120_000 }, async (context) => {
  const api = fixtureAPI(['alpha'])
  const { url, browser, evaluate, waitFor } = await startBrowser(context, 'notes-order', api.plugin)
  await browser(['open', `${url}agents/alpha`])
  await browser(['set', 'viewport', '1280', '700'])
  await evaluate(`localStorage.clear(), sessionStorage.clear(), ${JSON.stringify(seeded)}.forEach((stored) => localStorage.setItem('herder.web.notes.v1:record:' + stored.record.id, JSON.stringify(stored))), true`)
  await browser(['reload'])
  await waitFor(`document.querySelectorAll('.notes-rail-view .note-card').length === 30`)
  const texts = `[...document.querySelectorAll('.notes-rail-view .note-card p')].map((node) => node.textContent)`
  assert.deepEqual((await evaluate(texts) as string[]).slice(0, 2), ['fixture note 0', 'fixture note 1'], 'oldest at the top')
  assert.ok(await evaluate(`(() => { const rail = document.querySelector('.notes-rail-view'); return rail.scrollHeight > rail.clientHeight && rail.scrollTop === 0 })()`), 'the rail overflows and sits at the top')

  await evaluate(`document.querySelector('[aria-label="Add note to unassigned"]').click(), true`)
  await waitFor(`document.activeElement?.matches('[data-notes-quick-input="general"]')`)
  await browser(['keyboard', 'inserttext', 'the newest note'])
  await browser(['press', 'Enter'])
  await waitFor(`document.querySelectorAll('.notes-rail-view .note-card').length === 31`)
  assert.equal((await evaluate(texts) as string[]).at(-1), 'the newest note', 'a new note lands at the bottom')
  await waitFor(`(() => {
    const rail = document.querySelector('.notes-rail-view').getBoundingClientRect()
    const card = [...document.querySelectorAll('.notes-rail-view .note-card')].at(-1).getBoundingClientRect()
    return card.top >= rail.top && card.bottom <= rail.bottom + 1
  })()`)

  // Editing the top note leaves it on top.
  await evaluate(`document.querySelector('.notes-rail-view .note-card').dispatchEvent(new MouseEvent('dblclick', { bubbles: true })), true`)
  await waitFor(`document.activeElement?.matches('textarea[aria-label="Edit note comment"]')`)
  await evaluate(`document.activeElement.setSelectionRange(document.activeElement.value.length, document.activeElement.value.length), true`)
  await browser(['keyboard', 'inserttext', ', edited'])
  await browser(['press', 'Enter'])
  await waitFor(`!document.querySelector('textarea[aria-label="Edit note comment"]')`)
  const after = await evaluate(texts) as string[]
  assert.equal(after[0], 'fixture note 0, edited', 'an edit keeps its place')
  assert.equal(after.at(-1), 'the newest note')
})
