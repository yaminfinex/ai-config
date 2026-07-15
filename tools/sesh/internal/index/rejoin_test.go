package index

import (
	"testing"
	"time"

	"sesh/internal/store"
	"sesh/internal/wire"
)

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
