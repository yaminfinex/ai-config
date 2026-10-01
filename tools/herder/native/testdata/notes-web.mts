// Builds notes-web.json with herder web's own record and hand-off code: `node --experimental-strip-types testdata/notes-web.mts > testdata/notes-web.json`.
import { storedNoteToStateRow } from '../../web/src/features/notes/notesSync.ts'
import { noteTransferText } from '../../web/src/features/notes/notesPresentation.ts'
const note = { id: '0b7f3c1e-5a2d-4c8e-9f61-3d2a7b9e4c10', group: 'mupu', text: 'ask it to split this', quote: 'the reducer owns\nevery mutation', source: { kind: 'transcript', agent: 'mupu' }, created: 1790000000000, updated: 1790000000500 }
const plain = { id: '7c2e9a40-1b3f-4d6a-8e05-6f9b2c4d1a77', group: 'mupu', text: 'check the outbox after a 409', created: 1790000001000, updated: 1790000001000 }
const row = (n, w) => storedNoteToStateRow({ version: 1, writeID: w, record: n })
const tomb = storedNoteToStateRow({ version: 1, writeID: 'e4d1c2b3-0000-4000-8000-000000000003', record: { id: plain.id, deleted: true, updated: 1790000002000 } })
console.log(JSON.stringify({ rows: [row(note, 'a1b2c3d4-0000-4000-8000-000000000001'), row(plain, 'a1b2c3d4-0000-4000-8000-000000000002'), tomb], handoff: [noteTransferText(note), noteTransferText(plain)] }, null, 2))
