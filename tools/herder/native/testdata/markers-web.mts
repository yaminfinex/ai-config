// Builds markers-web.json with herder web's own read-marker code (RM): `node --experimental-strip-types testdata/markers-web.mts > testdata/markers-web.json`.
import { readFileSync } from 'node:fs'
import { mergeMarkerRow, markerStateRow, parseMarkerRow } from '../../web/src/features/spaces/readMarkerModel.ts'
import { markLastTurnUnread, readThrough } from '../../web/src/features/spaces/readPositionModel.ts'
import { spaceAttention } from '../../web/src/features/spaces/spaceAttentionModel.ts'

const board = JSON.parse(readFileSync(new URL('./fleet.json', import.meta.url), 'utf8'))
const panes = board.workspaces.flatMap((w) => w.tabs.flatMap((t) => t.panes)).concat(board.unplaced)
const pane = (name) => panes.find((p) => p.agent === name)
// fleet.json with these bus statuses.
const bus = { 'chief-mihe': 'blocked', 'e2e-cli-lipo': 'inactive' }
for (const [name, status] of Object.entries(bus)) pane(name).bus_status = status
const turn = (name) => pane(name).turn_end_id
const pos = (session, offset) => ({ session, offset, ts: '2026-10-04T00:00:00.000Z' })
const marker = (name, t, extra = {}) => ({ key: name, value: { turn: t, pos: null, at: 0, unread: false, updated: 5, ...extra }, updated: 5, writeID: `w-${name}`, deleted: false })
const rows = [
  marker('mupu', turn('mupu') - 1),
  marker('support-mifa', turn('support-mifa')),
  marker('risk-framework-gezu', turn('risk-framework-gezu'), { unread: true }),
  marker('api-gateway-luko', turn('api-gateway-luko') - 3),
  marker('chief-mihe', turn('chief-mihe') - 1),
  marker('e2e-cli-lipo', turn('e2e-cli-lipo') - 1),
  marker('grill-confirm-lubo', 0),
  marker('walk-nuna', turn('walk-nuna') - 1, { pos: pos('s1', 10), at: 9 }),
]
const agents = ['mupu', 'support-mifa', 'risk-framework-gezu', 'api-gateway-luko', 'chief-mihe', 'e2e-cli-lipo', 'grill-confirm-lubo', 'conductor-line', 'walk-nuna', 'nobody']
const markers = Object.fromEntries(rows.map((r) => { const m = parseMarkerRow(r); return [m.key, { turn: m.turn, pos: m.pos, at: m.at, unread: m.unread }] }))

const row = (key, updated, writeID, value) => ({ key, value: value && { ...value, updated }, updated, writeID, deleted: value === null })
const v = (turn, p, at, unread = false) => ({ turn, pos: p, at, unread })
const merges = [
  ['a newer stale read keeps the turn and position', row('a', 10, 'x', v(7, pos('s1', 300), 50)), row('a', 20, 'y', v(5, pos('s1', 100), 60))],
  ['an older row behind changes nothing', row('a', 20, 'x', v(7, pos('s1', 300), 50)), row('a', 10, 'y', v(5, pos('s1', 100), 40))],
  ['a newer mark unread stands as written', row('a', 10, 'x', v(7, pos('s1', 300), 50)), row('a', 20, 'y', v(5, pos('s1', 100), 40, true))],
  ['an older mark unread loses', row('a', 20, 'x', v(7, pos('s1', 300), 50)), row('a', 10, 'y', v(5, pos('s1', 100), 40, true))],
  ['across sessions the later read is ahead', row('a', 10, 'x', v(7, pos('s2', 5), 90)), row('a', 20, 'y', v(7, pos('s1', 900), 60))],
  ['a newer row ahead wins as is', row('a', 10, 'x', v(5, pos('s1', 100), 40)), row('a', 20, 'y', v(7, pos('s1', 300), 50))],
  ['the same version is idempotent', row('a', 10, 'x', v(5, null, 0)), row('a', 10, 'x', v(5, null, 0))],
  ['the writeID breaks a tie', row('a', 10, 'x', v(9, null, 0)), row('a', 10, 'y', v(5, null, 0))],
  ['a tombstone wins when newer', row('a', 10, 'x', v(5, null, 0)), row('a', 20, 'y', null)],
  ['a newer row beats a tombstone', row('a', 20, 'x', null), row('a', 30, 'y', v(1, null, 0))],
].map(([name, current, incoming]) => {
  const merged = mergeMarkerRow(parseMarkerRow(current), parseMarkerRow(incoming))
  return { name, current, incoming, merged: markerStateRow(merged.row), repair: merged.repair }
})

const reads = [
  ['no marker reads the turn and position', undefined, 7, pos('s1', 100), 1000],
  ['nothing new is nothing', v(7, pos('s1', 100), 900), 7, pos('s1', 100), 1000],
  ['a creep inside a read turn waits 5s', v(7, pos('s1', 100), 900), 7, pos('s1', 200), 5000],
  ['and writes after it', v(7, pos('s1', 100), 900), 7, pos('s1', 200), 5900],
  ['a turn end writes at once', v(7, pos('s1', 100), 900), 8, pos('s1', 200), 1000],
  ['a new session writes at once', v(7, pos('s1', 100), 900), 7, pos('s2', 3), 1000],
  ['a mark unread clears at once', v(7, pos('s1', 100), 900, true), 7, pos('s1', 100), 1000],
  ['no latest keeps the position', v(7, pos('s1', 100), 900), 8, null, 1000],
  ['the turn never goes back', v(9, pos('s1', 100), 900), 8, pos('s1', 50), 1000],
  ['no turn signal keeps the turn', v(4, null, 0), null, pos('s1', 1), 1000],
].map(([name, marker, t, latest, now]) => ({ name, marker: marker ?? null, turn: t, latest, now, read: readThrough(marker, t, latest, now) }))

// Where ⌥U puts reading back in mupu's recorded transcript: its whole history in one window, and only
// the last 100 entries (no opener among them).
const history = ['before', 'tail'].flatMap((page) => JSON.parse(readFileSync(new URL(`./agents/mupu/${page}.json`, import.meta.url), 'utf8')).entries)
const unreadAt = (entries) => markLastTurnUnread(undefined, { sessionId: 's1', entries }).pos
const turnStart = { whole: unreadAt(history), last100: unreadAt(history.slice(-100)) }

console.log(JSON.stringify({ bus, turnStart, rows, agents, attention: spaceAttention(board, agents, markers), merges, reads }, null, 2))
