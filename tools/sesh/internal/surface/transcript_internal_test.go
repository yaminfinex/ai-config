package surface

import (
	"testing"
	"time"

	"sesh/internal/wire"
)

// The frozen transcript ordering tuple: (timestamp_utc nulls last,
// file_ordinal, line_ordinal, file_uuid, generation). Never parentUuid
// chains (R16).
func TestSortTranscriptFrozenTuple(t *testing.T) {
	ts := func(sec int) *time.Time {
		v := time.Date(2026, 7, 1, 0, 0, sec, 0, time.UTC)
		return &v
	}
	rows := []wire.IndexMessage{
		{MessageUUID: "null-ts-late", TimestampUTC: nil, FileOrdinal: 0, LineOrdinal: 0},
		{MessageUUID: "t2", TimestampUTC: ts(2), FileOrdinal: 0, LineOrdinal: 5},
		{MessageUUID: "t1-file1", TimestampUTC: ts(1), FileOrdinal: 1, LineOrdinal: 0},
		{MessageUUID: "t1-file0-line3", TimestampUTC: ts(1), FileOrdinal: 0, LineOrdinal: 3},
		{MessageUUID: "t1-file0-line1", TimestampUTC: ts(1), FileOrdinal: 0, LineOrdinal: 1},
	}
	sortTranscript(rows)
	want := []string{"t1-file0-line1", "t1-file0-line3", "t1-file1", "t2", "null-ts-late"}
	for i, w := range want {
		if rows[i].MessageUUID != w {
			t.Fatalf("position %d = %s, want %s (frozen ordering tuple)", i, rows[i].MessageUUID, w)
		}
	}
}

func TestTruncateTextRuneBoundary(t *testing.T) {
	s := "aaaaé" // multibyte rune straddling the cut
	got, cut := truncateText(s, 5)
	if !cut {
		t.Fatal("expected truncation")
	}
	if got != "aaaa" {
		t.Fatalf("cut %q tore a rune", got)
	}
	if got, cut := truncateText("short", 100); cut || got != "short" {
		t.Fatal("under-limit text must pass through")
	}
}
