package index

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"sesh/internal/store"
	"sesh/internal/wire"
)

func TestVanishedOverlapRejoinsWithSurvivingQuarantine(t *testing.T) {
	for _, tc := range []struct {
		name          string
		vanishedFirst bool
	}{
		{name: "survivor arrives first"},
		{name: "vanished member arrives first", vanishedFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, idx := newHarness(t)
			originSession, resumedSession := syntheticUUID(97_020), syntheticUUID(97_021)
			originFile, resumedFile := syntheticUUID(97_120), syntheticUUID(97_121)
			start := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
			origin := syntheticSessionBody(originSession, "qpartial", 2, start)
			resumed := append(syntheticResumeBody(resumedSession, "unused", []string{"qpartial-00", "qpartial-01"}, 0, start.Add(time.Hour)), []byte("not json\n")...)

			if tc.vanishedFirst {
				appendAndIndex(t, st, idx, resumedSession, resumedFile, 0, resumed)
				if _, err := st.DB().ExecContext(t.Context(), `UPDATE files SET created_at = '9999-01-01T00:00:00Z' WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = 0`, wire.ToolClaude, resumedSession, resumedFile); err != nil {
					t.Fatal(err)
				}
				appendAndIndex(t, st, idx, originSession, originFile, 0, origin)
			} else {
				appendAndIndex(t, st, idx, originSession, originFile, 0, origin)
				appendAndIndex(t, st, idx, resumedSession, resumedFile, 0, resumed)
			}

			var all, normal int
			if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*), SUM(quarantine = 0)
				FROM sesh_index_messages INDEXED BY sesh_index_messages_file
				WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = 0`,
				wire.ToolClaude, resumedSession, resumedFile).Scan(&all, &normal); err != nil {
				t.Fatal(err)
			}
			if all != 1 || normal != 0 {
				t.Fatalf("mixed-history premise all/normal = %d/%d, want 1/0", all, normal)
			}
			var observed string
			if err := st.DB().QueryRowContext(t.Context(), `SELECT observed_at FROM quarantine_ledger
				WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = 0 AND line_ordinal = 2 AND reason = 'invalid_json'`,
				wire.ToolClaude, resumedSession, resumedFile).Scan(&observed); err != nil {
				t.Fatal(err)
			}
			var factsBefore int
			if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM fact_observations
				WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = 0`,
				wire.ToolClaude, resumedSession, resumedFile).Scan(&factsBefore); err != nil {
				t.Fatal(err)
			}

			tail := syntheticSessionBody(resumedSession, "qtail", 1, start.Add(2*time.Hour))
			appendAndIndex(t, st, idx, resumedSession, resumedFile, int64(len(resumed)), tail)
			assertQuarantineUnchanged(t, st, resumedSession, resumedFile, observed)
			assertFactObservations(t, st, resumedSession, resumedFile, factsBefore+1)
			incremental, incrementalRows, err := idx.Checksum(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if incremental != "dd1d79f24dccd99ade3c829975d6bafd24c1a013d7641cc49e58071fb2b4ad7e" || incrementalRows != 4 {
				t.Fatalf("mixed-history incremental checksum = %s/%d", incremental, incrementalRows)
			}
			if err := idx.Reindex(t.Context()); err != nil {
				t.Fatal(err)
			}
			reindexed, reindexedRows, err := idx.Checksum(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if reindexed != "dd1d79f24dccd99ade3c829975d6bafd24c1a013d7641cc49e58071fb2b4ad7e" || reindexedRows != 4 {
				t.Fatalf("reviewer red-chain fixture drifted: reindex %s/%d", reindexed, reindexedRows)
			}
			assertQuarantineUnchanged(t, st, resumedSession, resumedFile, observed)
			assertFactObservations(t, st, resumedSession, resumedFile, factsBefore+1)
			if err := idx.Reindex(t.Context()); err != nil {
				t.Fatal(err)
			}
			fixed, fixedRows, err := idx.Checksum(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if reindexed != fixed || reindexedRows != fixedRows {
				t.Fatalf("reindex is not fixed: %s/%d then %s/%d", reindexed, reindexedRows, fixed, fixedRows)
			}
			if incremental != reindexed || incrementalRows != reindexedRows {
				t.Fatalf("incremental %s/%d does not match reindex %s/%d", incremental, incrementalRows, reindexed, reindexedRows)
			}
		})
	}
}

func assertFactObservations(t *testing.T, st *store.Store, session, file string, want int) {
	t.Helper()
	var got int
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM fact_observations
		WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = 0`,
		wire.ToolClaude, session, file).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("fact observations = %d, want append-only PUT count %d", got, want)
	}
}

func assertQuarantineUnchanged(t *testing.T, st *store.Store, session, file, observed string) {
	t.Helper()
	var messages, ledger int
	var gotObserved string
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sesh_index_messages INDEXED BY sesh_index_messages_file
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = 0 AND quarantine = 1`,
		wire.ToolClaude, session, file).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*), MIN(observed_at) FROM quarantine_ledger
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = 0 AND line_ordinal = 2 AND reason = 'invalid_json'`,
		wire.ToolClaude, session, file).Scan(&ledger, &gotObserved); err != nil {
		t.Fatal(err)
	}
	if messages != 1 || ledger != 1 || gotObserved != observed {
		t.Fatalf("quarantine changed: messages=%d ledger=%d observed=%q, want 1/1/%q", messages, ledger, gotObserved, observed)
	}
}

func TestJunkOnlyHistoryReplayKeepsOneObservation(t *testing.T) {
	st, idx := newHarness(t)
	session, file := syntheticUUID(97_030), syntheticUUID(97_130)
	junk := []byte("not json\n")
	appendAndIndex(t, st, idx, session, file, 0, junk)
	var observed string
	if err := st.DB().QueryRowContext(t.Context(), `SELECT observed_at FROM quarantine_ledger
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = 0 AND line_ordinal = 0 AND reason = 'invalid_json'`,
		wire.ToolClaude, session, file).Scan(&observed); err != nil {
		t.Fatal(err)
	}

	capture, restore := captureAppendPhases(t)
	tail := syntheticSessionBody(session, "junk-tail", 1, time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC))
	appendAndIndex(t, st, idx, session, file, int64(len(junk)), tail)
	restore()
	if got := capture.intSum("rows"); got != 1 {
		t.Fatalf("junk-only append inserted %d rows, want only the new tail", got)
	}
	assertQuarantineUnchangedAtLine(t, st, session, file, 0, observed)
	assertChecksumMatchesReindex(t, idx)
	assertReindexFixedPoint(t, idx)
}

func TestQuarantinedGenerationReplayIsGenerationScoped(t *testing.T) {
	st, idx := newHarness(t)
	session, file := syntheticUUID(97_040), syntheticUUID(97_140)
	gen0 := syntheticSessionBody(session, "generation-zero", 1, time.Date(2026, 7, 15, 7, 0, 0, 0, time.UTC))
	appendAndIndex(t, st, idx, session, file, 0, gen0)

	junk := []byte("not json\n")
	path := st.MirrorPath(wire.ToolClaude, session, file, 1)
	if err := os.WriteFile(path, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().ExecContext(t.Context(), `INSERT INTO files
		(tool, session_id, file_uuid, generation, high_water, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, 'later', 'later')`, wire.ToolClaude, session, file, len(junk)); err != nil {
		t.Fatal(err)
	}
	gen1 := wire.AppendEvent{Tool: wire.ToolClaude, WireSessionID: session, FileUUID: file, Generation: 1, ByteEnd: int64(len(junk))}
	if err := idx.ProcessAppend(t.Context(), gen1); err != nil {
		t.Fatal(err)
	}
	var observed string
	if err := st.DB().QueryRowContext(t.Context(), `SELECT observed_at FROM quarantine_ledger
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = 1 AND line_ordinal = 0 AND reason = 'invalid_json'`,
		wire.ToolClaude, session, file).Scan(&observed); err != nil {
		t.Fatal(err)
	}

	tail := syntheticSessionBody(session, "generation-one", 1, time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC))
	if err := os.WriteFile(path, append(append([]byte{}, junk...), tail...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().ExecContext(t.Context(), `UPDATE files SET high_water = ?
		WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = 1`, len(junk)+len(tail), wire.ToolClaude, session, file); err != nil {
		t.Fatal(err)
	}
	capture, restore := captureAppendPhases(t)
	gen1.ByteStart = int64(len(junk))
	gen1.ByteEnd = int64(len(junk) + len(tail))
	if err := idx.ProcessAppend(t.Context(), gen1); err != nil {
		t.Fatal(err)
	}
	restore()
	if got := capture.intSum("rows"); got != 1 {
		t.Fatalf("quarantined generation inserted %d rows, want only its new tail", got)
	}
	assertQuarantineUnchangedAtGeneration(t, st, session, file, 1, 0, observed)
	assertChecksumMatchesReindex(t, idx)
	assertReindexFixedPoint(t, idx)
}

func TestPiHeaderOnlyAppendDoesNotReplayHeader(t *testing.T) {
	st, idx := newHarness(t)
	header := []byte(`{"type":"session","id":"` + piSID + `"}` + "\n")
	rr, event := putPi(t, st, 0, header)
	if rr.Code != 200 || event == nil {
		t.Fatalf("header PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := idx.ProcessAppend(t.Context(), *event); err != nil {
		t.Fatal(err)
	}

	tail := []byte(`{"type":"message","id":"pi-tail","parentId":null,"message":{"role":"user","content":"x"}}` + "\n")
	rr, event = putPi(t, st, int64(len(header)), tail)
	if rr.Code != 200 || event == nil {
		t.Fatalf("tail PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	capture, restore := captureAppendPhases(t)
	if err := idx.ProcessAppend(t.Context(), *event); err != nil {
		t.Fatal(err)
	}
	restore()
	if got := capture.intSum("rows"); got != 1 {
		t.Fatalf("Pi header-only append indexed %d rows, want only the new tail", got)
	}
	var headers int
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sesh_index_messages
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = 0 AND entry_type = 'session'`,
		wire.ToolPi, piSID, piSID).Scan(&headers); err != nil {
		t.Fatal(err)
	}
	if headers != 1 {
		t.Fatalf("Pi header rows = %d, want 1", headers)
	}
	assertChecksumMatchesReindex(t, idx)
	assertReindexFixedPoint(t, idx)
}

func captureAppendPhases(t *testing.T) (*phaseCapture, func()) {
	t.Helper()
	capture := &phaseCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	restore := func() { slog.SetDefault(previous) }
	t.Cleanup(restore)
	return capture, restore
}

func assertQuarantineUnchangedAtLine(t *testing.T, st *store.Store, session, file string, line int64, observed string) {
	t.Helper()
	assertQuarantineUnchangedAtGeneration(t, st, session, file, 0, line, observed)
}

func assertQuarantineUnchangedAtGeneration(t *testing.T, st *store.Store, session, file string, generation int, line int64, observed string) {
	t.Helper()
	var messages, ledger int
	var gotObserved string
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sesh_index_messages INDEXED BY sesh_index_messages_file
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ? AND line_ordinal = ? AND quarantine = 1`,
		wire.ToolClaude, session, file, generation, line).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*), MIN(observed_at) FROM quarantine_ledger
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ? AND line_ordinal = ? AND reason = 'invalid_json'`,
		wire.ToolClaude, session, file, generation, line).Scan(&ledger, &gotObserved); err != nil {
		t.Fatal(err)
	}
	if messages != 1 || ledger != 1 || gotObserved != observed {
		t.Fatalf("quarantine changed: messages=%d ledger=%d observed=%q, want 1/1/%q", messages, ledger, gotObserved, observed)
	}
}

func appendAndIndex(t *testing.T, st *store.Store, idx *Indexer, session, file string, offset int64, body []byte) {
	t.Helper()
	putBytes(t, st, session, file, offset, body)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
}

func assertFileVanished(t *testing.T, st *store.Store, session, file string) {
	t.Helper()
	var rows int
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COUNT(*)
		FROM sesh_index_messages INDEXED BY sesh_index_messages_file
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = 0`,
		wire.ToolClaude, session, file).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("fixture premise failed: vanished file retained %d rows", rows)
	}
}

func assertReindexFixedPoint(t *testing.T, idx *Indexer) {
	t.Helper()
	first, firstRows, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	second, secondRows, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || firstRows != secondRows {
		t.Fatalf("reindex fixed point changed %s/%d to %s/%d", first, firstRows, second, secondRows)
	}
}

func TestMultipleVanishedMembersRejoinTheirLogicalSession(t *testing.T) {
	st, idx := newHarness(t)
	originSession, firstSession, secondSession := syntheticUUID(99_000), syntheticUUID(99_001), syntheticUUID(99_002)
	originFile, firstFile, secondFile := syntheticUUID(99_100), syntheticUUID(99_101), syntheticUUID(99_102)
	start := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	origin := syntheticSessionBody(originSession, "multi", 4, start)
	first := syntheticResumeBody(firstSession, "unused", []string{"multi-00", "multi-01"}, 0, start.Add(time.Hour))
	second := syntheticResumeBody(secondSession, "unused", []string{"multi-02", "multi-03"}, 0, start.Add(2*time.Hour))

	appendAndIndex(t, st, idx, originSession, originFile, 0, origin)
	appendAndIndex(t, st, idx, firstSession, firstFile, 0, first)
	appendAndIndex(t, st, idx, secondSession, secondFile, 0, second)
	assertFileVanished(t, st, firstSession, firstFile)
	assertFileVanished(t, st, secondSession, secondFile)

	firstTail := syntheticSessionBody(firstSession, "first-tail", 1, start.Add(3*time.Hour))
	secondTail := syntheticSessionBody(secondSession, "second-tail", 1, start.Add(4*time.Hour))
	appendAndIndex(t, st, idx, firstSession, firstFile, int64(len(first)), firstTail)
	appendAndIndex(t, st, idx, secondSession, secondFile, int64(len(second)), secondTail)
	assertOneLogicalSession(t, st, originSession, []string{originFile, firstFile, secondFile})
	assertChecksumMatchesReindex(t, idx)
	assertReindexFixedPoint(t, idx)
}

func TestVanishedMemberReplayIncludesKeysAcrossAppendPasses(t *testing.T) {
	st, idx := newHarness(t)
	originSession, resumedSession := syntheticUUID(99_010), syntheticUUID(99_011)
	originFile, resumedFile := syntheticUUID(99_110), syntheticUUID(99_111)
	start := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	origin := syntheticSessionBody(originSession, "straddle", 2, start)
	first := syntheticResumeBody(resumedSession, "unused", []string{"straddle-00"}, 0, start.Add(time.Hour))
	second := syntheticResumeBody(resumedSession, "unused", []string{"straddle-01"}, 0, start.Add(2*time.Hour))

	appendAndIndex(t, st, idx, originSession, originFile, 0, origin)
	appendAndIndex(t, st, idx, resumedSession, resumedFile, 0, first)
	appendAndIndex(t, st, idx, resumedSession, resumedFile, int64(len(first)), second)
	assertFileVanished(t, st, resumedSession, resumedFile)

	tail := syntheticSessionBody(resumedSession, "straddle-tail", 1, start.Add(3*time.Hour))
	appendAndIndex(t, st, idx, resumedSession, resumedFile, int64(len(first)+len(second)), tail)
	assertOneLogicalSession(t, st, originSession, []string{originFile, resumedFile})
	assertChecksumMatchesReindex(t, idx)
	assertReindexFixedPoint(t, idx)
}

func TestVanishedMemberRejoinsOrdinalCompactedGroup(t *testing.T) {
	st, idx := newHarness(t)
	originSession, vanishedSession, lastSession := syntheticUUID(99_020), syntheticUUID(99_021), syntheticUUID(99_022)
	originFile, vanishedFile, lastFile := syntheticUUID(99_120), syntheticUUID(99_121), syntheticUUID(99_122)
	start := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	origin := syntheticSessionBody(originSession, "compact", 4, start)
	vanished := syntheticResumeBody(vanishedSession, "unused", []string{"compact-00", "compact-01"}, 0, start.Add(time.Hour))
	last := syntheticResumeBody(lastSession, "last", []string{"compact-02", "compact-03"}, 1, start.Add(2*time.Hour))

	appendAndIndex(t, st, idx, originSession, originFile, 0, origin)
	appendAndIndex(t, st, idx, vanishedSession, vanishedFile, 0, vanished)
	appendAndIndex(t, st, idx, lastSession, lastFile, 0, last)
	assertFileVanished(t, st, vanishedSession, vanishedFile)
	var compacted int64
	if err := st.DB().QueryRowContext(t.Context(), `SELECT MIN(file_ordinal) FROM sesh_index_messages WHERE file_uuid = ?`, lastFile).Scan(&compacted); err != nil {
		t.Fatal(err)
	}
	if compacted != 1 {
		t.Fatalf("fixture premise failed: later survivor ordinal = %d, want compacted ordinal 1", compacted)
	}

	tail := syntheticSessionBody(vanishedSession, "compact-tail", 1, start.Add(3*time.Hour))
	appendAndIndex(t, st, idx, vanishedSession, vanishedFile, int64(len(vanished)), tail)
	assertOneLogicalSession(t, st, originSession, []string{originFile, vanishedFile, lastFile})
	for file, want := range map[string]int64{originFile: 0, vanishedFile: 1, lastFile: 2} {
		var got int64
		if err := st.DB().QueryRowContext(t.Context(), `SELECT MIN(file_ordinal) FROM sesh_index_messages WHERE file_uuid = ?`, file).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("file %s ordinal = %d, want %d", file, got, want)
		}
	}
	assertChecksumMatchesReindex(t, idx)
	assertReindexFixedPoint(t, idx)
}

func TestVanishedMemberRejoinsAfterIndexerRestart(t *testing.T) {
	dir := t.TempDir()
	open := func() (*store.Store, *Indexer) {
		t.Helper()
		st, err := store.Open(t.Context(), store.Config{Dir: dir, AppendBuffer: 32})
		if err != nil {
			t.Fatal(err)
		}
		idx, err := New(t.Context(), st.DB(), st.MirrorPath)
		if err != nil {
			_ = st.Close()
			t.Fatal(err)
		}
		return st, idx
	}

	originSession, vanishedSession := syntheticUUID(99_030), syntheticUUID(99_031)
	originFile, vanishedFile := syntheticUUID(99_130), syntheticUUID(99_131)
	start := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	origin := syntheticSessionBody(originSession, "restart", 2, start)
	vanished := syntheticResumeBody(vanishedSession, "unused", []string{"restart-00", "restart-01"}, 0, start.Add(time.Hour))

	st, idx := open()
	appendAndIndex(t, st, idx, originSession, originFile, 0, origin)
	appendAndIndex(t, st, idx, vanishedSession, vanishedFile, 0, vanished)
	assertFileVanished(t, st, vanishedSession, vanishedFile)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, idx = open()
	t.Cleanup(func() { _ = st.Close() })
	tail := syntheticSessionBody(vanishedSession, "restart-tail", 1, start.Add(2*time.Hour))
	appendAndIndex(t, st, idx, vanishedSession, vanishedFile, int64(len(vanished)), tail)
	assertOneLogicalSession(t, st, originSession, []string{originFile, vanishedFile})
	assertChecksumMatchesReindex(t, idx)
	assertReindexFixedPoint(t, idx)
}
