package index

// Differential equivalence gate for the bounded-append work: the optimized
// maintenance (pinned plans, no-op-free rewrites) must produce byte-identical
// index outcomes to the naive pre-optimization shapes it replaced. A churned
// fixture corpus — resume unification, transitive chains, canonical-order
// stress, codex inheritance, quarantine, generation churn, trailing partials,
// near-miss overlaps — is replayed through both paths and the resulting
// tables must match; the incremental result must also match a full Reindex.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sesh/internal/store"
	"sesh/internal/wire"
)

// churnedCorpusIDs collects the identities the churn scenario asserts on.
type churnedCorpusIDs struct {
	origSession, resumeSession   string
	origFile, resumeFile         string
	chainASession                string
	chainAFile, chainBFile       string
	chainCFile                   string
	earlyOrigSession, earlyOrig  string
	earlyResumeSess, earlyResume string
}

// driveChurnedCorpus replays one deterministic churned append sequence
// against the given harness. Both differential legs run exactly this
// sequence.
func driveChurnedCorpus(t *testing.T, st *store.Store, idx *Indexer) churnedCorpusIDs {
	t.Helper()
	process := func() {
		t.Helper()
		if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
			t.Fatal(err)
		}
	}

	// Unrelated singles: the corpus the maintenance must not touch.
	for i := 0; i < 10; i++ {
		id := syntheticUUID(90_000 + i)
		body := syntheticSessionBody(id, fmt.Sprintf("churn-single-%02d", i), 5, time.Date(2026, 7, 10, 10, i, 0, 0, time.UTC))
		putBytes(t, st, id, id, 0, body)
		process()
	}

	ids := churnedCorpusIDs{
		origSession:      syntheticUUID(90_100),
		resumeSession:    syntheticUUID(90_101),
		origFile:         syntheticUUID(91_100),
		resumeFile:       syntheticUUID(91_101),
		chainASession:    syntheticUUID(90_300),
		chainAFile:       syntheticUUID(91_300),
		chainBFile:       syntheticUUID(91_301),
		chainCFile:       syntheticUUID(91_302),
		earlyOrigSession: syntheticUUID(90_400),
		earlyOrig:        syntheticUUID(91_400),
		earlyResumeSess:  syntheticUUID(90_399),
		earlyResume:      syntheticUUID(91_399),
	}

	// Resume pair, then steady-state tails onto the unified group.
	origBody := syntheticSessionBody(ids.origSession, "churn-orig", 8, time.Date(2026, 7, 10, 11, 0, 0, 0, time.UTC))
	resumeBody := syntheticResumeBody(ids.resumeSession, "churn-resume", []string{"churn-orig-02", "churn-orig-03"}, 5, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	putBytes(t, st, ids.origSession, ids.origFile, 0, origBody)
	process()
	putBytes(t, st, ids.resumeSession, ids.resumeFile, 0, resumeBody)
	process()
	offset := int64(len(resumeBody))
	for i := 0; i < 3; i++ {
		tail := []byte(fmt.Sprintf(
			`{"type":"message","uuid":"churn-tail-%02d","sessionId":"%s","timestamp":"2026-07-10T13:00:%02dZ","message":{"role":"user"}}`+"\n",
			i, ids.resumeSession, i))
		putBytes(t, st, ids.resumeSession, ids.resumeFile, offset, tail)
		offset += int64(len(tail))
		process()
	}

	// Resume id that sorts before the canonical original: canonical choice
	// must come from ingest order, not id order.
	earlyOrigBody := syntheticSessionBody(ids.earlyOrigSession, "churn-early", 6, time.Date(2026, 7, 10, 14, 0, 0, 0, time.UTC))
	earlyResumeBody := syntheticResumeBody(ids.earlyResumeSess, "churn-early-resume", []string{"churn-early-02", "churn-early-03"}, 3, time.Date(2026, 7, 10, 15, 0, 0, 0, time.UTC))
	putBytes(t, st, ids.earlyOrigSession, ids.earlyOrig, 0, earlyOrigBody)
	process()
	putBytes(t, st, ids.earlyResumeSess, ids.earlyResume, 0, earlyResumeBody)
	process()

	// Transitive chain: a <- b, bridge rows into b, then c resumes from the
	// bridge — the append after unification must bridge the whole component.
	bSession := syntheticUUID(90_299)
	cSession := syntheticUUID(90_301)
	aBody := syntheticSessionBody(ids.chainASession, "churn-chain-a", 6, time.Date(2026, 7, 10, 16, 0, 0, 0, time.UTC))
	bBody := syntheticResumeBody(bSession, "churn-chain-b", []string{"churn-chain-a-02", "churn-chain-a-03"}, 2, time.Date(2026, 7, 10, 17, 0, 0, 0, time.UTC))
	putBytes(t, st, ids.chainASession, ids.chainAFile, 0, aBody)
	process()
	putBytes(t, st, bSession, ids.chainBFile, 0, bBody)
	process()
	bridge := []byte(
		`{"type":"message","uuid":"churn-bridge-00","sessionId":"` + bSession + `","timestamp":"2026-07-10T18:00:00Z","message":{"role":"user"}}` + "\n" +
			`{"type":"message","uuid":"churn-bridge-01","sessionId":"` + bSession + `","timestamp":"2026-07-10T18:00:01Z","message":{"role":"assistant"}}` + "\n")
	putBytes(t, st, bSession, ids.chainBFile, int64(len(bBody)), bridge)
	process()
	cBody := syntheticResumeBody(cSession, "churn-chain-c", []string{"churn-bridge-00", "churn-bridge-01"}, 2, time.Date(2026, 7, 10, 19, 0, 0, 0, time.UTC))
	putBytes(t, st, cSession, ids.chainCFile, 0, cBody)
	process()

	// Codex: session_meta drives the meta row's logical id, items keep the
	// wire id, delivered across two chunks.
	codexWire := syntheticUUID(90_500)
	codexFile := syntheticUUID(91_500)
	meta := codexSessionMetaLine("churn-codex-payload")
	putToolBytes(t, st, wire.ToolCodex, codexWire, codexFile, 0, []byte(meta))
	process()
	items := codexResponseItemLine("churn-codex-item-1") + codexResponseItemLine("churn-codex-item-2")
	putToolBytes(t, st, wire.ToolCodex, codexWire, codexFile, int64(len(meta)), []byte(items))
	process()

	// Quarantine churn: unparseable lines interleaved with good ones.
	qSession := syntheticUUID(90_600)
	qBody := []byte("[]\nnot-json\n" +
		`{"type":"message","uuid":"churn-q-good","sessionId":"` + qSession + `","timestamp":"2026-07-10T20:00:00Z","message":{"role":"user"}}` + "\n")
	putBytes(t, st, qSession, qSession, 0, qBody)
	process()

	// Generation churn: the same bytes re-mirrored as generation 1 must
	// dedupe against generation 0 (generation is absent from the dedup key).
	genSession := syntheticUUID(90_700)
	genFile := syntheticUUID(91_700)
	genBody := syntheticSessionBody(genSession, "churn-gen", 6, time.Date(2026, 7, 10, 21, 0, 0, 0, time.UTC))
	putBytes(t, st, genSession, genFile, 0, genBody)
	process()
	gen1Path := st.MirrorPath(wire.ToolClaude, genSession, genFile, 1)
	if err := os.MkdirAll(filepath.Dir(gen1Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gen1Path, genBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO files(tool, session_id, file_uuid, generation, high_water, created_at, updated_at) VALUES (?, ?, ?, 1, ?, '2026-07-10T21:30:00Z', '2026-07-10T21:30:00Z')`,
		wire.ToolClaude, genSession, genFile, len(genBody)); err != nil {
		t.Fatal(err)
	}
	if err := idx.ProcessAppend(t.Context(), wire.AppendEvent{
		Tool: wire.ToolClaude, WireSessionID: genSession, FileUUID: genFile,
		Generation: 1, ByteStart: 0, ByteEnd: int64(len(genBody)),
	}); err != nil {
		t.Fatal(err)
	}

	// Trailing partial: a mid-line chunk held back, then completed.
	partSession := syntheticUUID(90_800)
	partBody := syntheticSessionBody(partSession, "churn-part", 4, time.Date(2026, 7, 10, 22, 0, 0, 0, time.UTC))
	cut := bytes.IndexByte(partBody, '\n') + 20
	putBytes(t, st, partSession, partSession, 0, partBody[:cut])
	process()
	putBytes(t, st, partSession, partSession, int64(cut), partBody[cut:])
	process()

	// Near-miss overlap: exactly one shared uuid must NOT unify.
	nearA := syntheticUUID(90_900)
	nearB := syntheticUUID(90_901)
	nearABody := []byte(
		`{"type":"message","uuid":"churn-near-shared","sessionId":"` + nearA + `","timestamp":"2026-07-10T23:00:00Z","message":{"role":"user"}}` + "\n" +
			`{"type":"message","uuid":"churn-near-a","sessionId":"` + nearA + `","timestamp":"2026-07-10T23:00:01Z","message":{"role":"assistant"}}` + "\n")
	nearBBody := []byte(
		`{"type":"message","uuid":"churn-near-shared","sessionId":"` + nearB + `","timestamp":"2026-07-10T23:10:00Z","message":{"role":"user"}}` + "\n" +
			`{"type":"message","uuid":"churn-near-b","sessionId":"` + nearB + `","timestamp":"2026-07-10T23:10:01Z","message":{"role":"assistant"}}` + "\n")
	putBytes(t, st, nearA, nearA, 0, nearABody)
	process()
	putBytes(t, st, nearB, nearB, 0, nearBBody)
	process()

	return ids
}

func dumpTable(t *testing.T, idx *Indexer, query string) []string {
	t.Helper()
	rows, err := idx.queryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		line := ""
		for _, v := range vals {
			line += fmt.Sprintf("%v\x1f", v)
		}
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOptimizedMaintenanceMatchesNaiveReferenceOverChurnedCorpus(t *testing.T) {
	stNew, idxNew := newHarness(t)
	idsNew := driveChurnedCorpus(t, stNew, idxNew)

	stOld, idxOld := newHarness(t)
	idxOld.naiveMaintenance = true
	driveChurnedCorpus(t, stOld, idxOld)

	newSum, newRows, err := idxNew.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	oldSum, oldRows, err := idxOld.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if newSum != oldSum || newRows != oldRows {
		t.Fatalf("optimized maintenance diverged from naive reference: %s/%d vs %s/%d", newSum, newRows, oldSum, oldRows)
	}

	const stateDump = `SELECT tool, wire_session_id, file_uuid, generation, complete_offset
		FROM index_file_state ORDER BY tool, wire_session_id, file_uuid, generation`
	newState := dumpTable(t, idxNew, stateDump)
	oldState := dumpTable(t, idxOld, stateDump)
	if fmt.Sprint(newState) != fmt.Sprint(oldState) {
		t.Fatalf("index_file_state diverged:\nnew: %v\nold: %v", newState, oldState)
	}
	// observed_at/day are wall-clock; only the identity columns must match.
	const ledgerDump = `SELECT tool, wire_session_id, file_uuid, generation, line_ordinal, reason
		FROM quarantine_ledger ORDER BY tool, wire_session_id, file_uuid, generation, line_ordinal, reason`
	newLedger := dumpTable(t, idxNew, ledgerDump)
	oldLedger := dumpTable(t, idxOld, ledgerDump)
	if fmt.Sprint(newLedger) != fmt.Sprint(oldLedger) {
		t.Fatalf("quarantine_ledger diverged:\nnew: %v\nold: %v", newLedger, oldLedger)
	}

	// The scenario must actually churn, or equivalence is vacuous: resume
	// unification landed, quarantine recorded, and dedupe deleted rows.
	assertOneLogicalSession(t, stNew, idsNew.origSession, []string{idsNew.origFile, idsNew.resumeFile})
	assertOneLogicalSession(t, stNew, idsNew.chainASession, []string{idsNew.chainAFile, idsNew.chainBFile, idsNew.chainCFile})
	assertOneLogicalSession(t, stNew, idsNew.earlyOrigSession, []string{idsNew.earlyOrig, idsNew.earlyResume})
	if len(newLedger) == 0 {
		t.Fatal("churned corpus recorded no quarantine rows")
	}
	var dupDeleted int
	if err := stNew.DB().QueryRow(`SELECT COUNT(*) FROM sesh_index_messages
		WHERE quarantine = 0 AND message_uuid IN ('churn-orig-02', 'churn-orig-03')`).Scan(&dupDeleted); err != nil {
		t.Fatal(err)
	}
	if dupDeleted != 2 {
		t.Fatalf("resume-shared uuids kept %d rows, want 2 (dedupe never ran)", dupDeleted)
	}

	// The incremental result must also be what a from-scratch rebuild
	// produces over the same churned corpus.
	assertChecksumMatchesReindex(t, idxNew)
}
