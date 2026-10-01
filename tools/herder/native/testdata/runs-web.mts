// Builds runs-web.json with herder web's own compact grouping: `node --experimental-strip-types testdata/runs-web.mts > testdata/runs-web.json`.
// Per agent, over its recorded pages (before, then tail): each row as `e:<byteOffset>` (a standalone entry) or a run's pills, `label` or `label ×n`.
// Web's unpaired `tool result` pills are dropped (native holds an orphan result until its call pages in).
import { readFileSync } from 'node:fs'
import { cleanRows, messageText, objectValue, valueText } from '../../web/src/features/transcript/cleanRows.ts'
import { aggregateActivityPills } from '../../web/src/features/transcript/cleanView.ts'
import { duplicateHcomDeliveryIndices } from '../../web/src/messagePolish.ts'

// TranscriptEntries.tsx's relateEntries, the part cleanRows reads.
function relate(entries) {
  const id = (e) => valueText(objectValue(e.payload).tool_use_id)
  const uses = new Set(entries.filter((e) => e.kind === 'tool_use').map(id))
  const pairedToolResults = new Set(entries.flatMap((e, i) => e.kind === 'tool_result' && uses.has(id(e)) ? [i] : []))
  const pairedDeliveries = new Set(entries.flatMap((e, i) => e.kind === 'hcom_delivery_stub' && entries[i + 1]?.kind === 'hcom_delivery' ? [i + 1] : []))
  const pairedCommandOutputs = new Set(entries.flatMap((e, i) => {
    const next = entries[i + 1]
    return e.kind === 'command_stdout' && messageText(e.payload).includes('<command-name>') && next?.kind === 'command_stdout' && messageText(next.payload).includes('<local-command-stdout>') ? [i + 1] : []
  }))
  return { pairedToolResults, pairedDeliveries, pairedCommandOutputs, duplicateHcomDeliveries: duplicateHcomDeliveryIndices(entries) }
}

const out = {}
for (const agent of ['mupu', 'conductor-line', 'grill-confirm-lubo', 'riko']) {
  const page = (p) => JSON.parse(readFileSync(new URL(`agents/${agent}/${p}.json`, import.meta.url), 'utf8')).entries
  const entries = [...page('before'), ...page('tail')]
  out[agent] = cleanRows(entries, relate(entries)).flatMap((row) => {
    if (row.type === 'entry') return [`e:${row.entry.byteOffset}`]
    const pills = aggregateActivityPills(row.activities.filter((a) => a.label !== 'tool result'))
    return pills.length ? [pills.map((p) => p.count > 1 ? `${p.label} ×${p.count}` : p.label)] : []
  })
}
console.log(JSON.stringify(out, null, 1))
