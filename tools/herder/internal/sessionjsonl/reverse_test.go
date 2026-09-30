package sessionjsonl

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanCompleteReverseIgnoresPartialTailAndStops(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "invented.jsonl")
	content := "invented-one\r\ninvented-two\ninvented-partial"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var lines []string
	_, err := ScanCompleteReverse(path, func(line []byte) bool {
		lines = append(lines, string(line))
		return len(lines) < 2
	})
	if err != nil || !reflect.DeepEqual(lines, []string{"invented-two", "invented-one"}) {
		t.Fatalf("lines = %#v, err = %v", lines, err)
	}
}

func TestCompleteEndStopsAfterLastNewline(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		content string
		want    int64
	}{
		{name: "empty"},
		{name: "one complete line", content: "invented-one\n", want: int64(len("invented-one\n"))},
		{name: "complete CRLF", content: "invented-one\r\n", want: int64(len("invented-one\r\n"))},
		{name: "partial tail", content: "invented-one\ninvented-partial", want: int64(len("invented-one\n"))},
		{name: "only partial", content: "invented-partial"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "invented.jsonl")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := CompleteEnd(path)
			if err != nil || got != test.want {
				t.Fatalf("complete end = %d, %v; want %d", got, err, test.want)
			}
		})
	}
}

func TestScanCompleteTailReportsStableLinesAndOffsets(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "invented.jsonl")
	content := "invented-one\r\ninvented-two\ninvented-partial"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	type record struct {
		body   string
		line   int64
		offset int64
	}
	var records []record
	end, reachedStart, err := ScanCompleteTail(path, func(raw []byte, line, offset int64) bool {
		records = append(records, record{body: string(raw), line: line, offset: offset})
		return true
	})
	wantRecords := []record{
		{body: "invented-two", line: 1, offset: int64(len("invented-one\r\n"))},
		{body: "invented-one", line: 0, offset: 0},
	}
	if err != nil || !reachedStart || end != int64(len("invented-one\r\ninvented-two\n")) {
		t.Fatalf("tail scan end = %d, %v, %v", end, reachedStart, err)
	}
	if !reflect.DeepEqual(records, wantRecords) {
		t.Fatalf("tail scan records=%#v", records)
	}
	records = nil
	_, reachedStart, err = ScanCompleteTail(path, func(raw []byte, line, offset int64) bool {
		records = append(records, record{body: string(raw), line: line, offset: offset})
		return false
	})
	if err != nil || reachedStart || !reflect.DeepEqual(records, wantRecords[:1]) {
		t.Fatalf("stopped tail scan = %#v, %v, %v", records, reachedStart, err)
	}
}

func TestScanCompleteTailFailsClosedWhenSnapshotIsTruncated(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "invented.jsonl")
	if err := os.WriteFile(path, []byte("invented-one\ninvented-two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	truncated := false
	_, _, err := ScanCompleteTail(path, func([]byte, int64, int64) bool {
		if !truncated {
			truncated = true
			if truncateErr := os.Truncate(path, 0); truncateErr != nil {
				t.Fatal(truncateErr)
			}
		}
		return true
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("tail scan error = %v; want unexpected EOF", err)
	}
}

func TestScanCompleteBeforeWalksRecordBoundaries(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "invented.jsonl")
	content := "invented-one\r\ninvented-two\ninvented-three\ninvented-partial"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	two := int64(len("invented-one\r\n"))
	three := two + int64(len("invented-two\n"))
	end := three + int64(len("invented-three\n"))
	type record struct {
		body   string
		line   int64
		offset int64
	}
	scan := func(before int64, keep int) ([]record, bool, error) {
		var records []record
		reachedStart, err := ScanCompleteBefore(path, before, func(raw []byte, line, offset int64) bool {
			records = append(records, record{body: string(raw), line: line, offset: offset})
			return len(records) < keep
		})
		return records, reachedStart, err
	}
	records, reachedStart, err := scan(end, 1)
	if err != nil || reachedStart || !reflect.DeepEqual(records, []record{{"invented-three", 2, three}}) {
		t.Fatalf("before end = %#v, %v, %v", records, reachedStart, err)
	}
	records, reachedStart, err = scan(three, 5)
	want := []record{{"invented-two", 1, two}, {"invented-one", 0, 0}}
	if err != nil || !reachedStart || !reflect.DeepEqual(records, want) {
		t.Fatalf("before three = %#v, %v, %v", records, reachedStart, err)
	}
	records, reachedStart, err = scan(two, 1)
	if err != nil || !reachedStart || !reflect.DeepEqual(records, want[1:]) {
		t.Fatalf("before two stopping on the first record = %#v, %v, %v", records, reachedStart, err)
	}
	records, reachedStart, err = scan(0, 5)
	if err != nil || !reachedStart || records != nil {
		t.Fatalf("before zero = %#v, %v, %v", records, reachedStart, err)
	}
	for _, before := range []int64{-1, 1, two - 1, end + 1, int64(len(content))} {
		if _, _, err := scan(before, 5); !errors.Is(err, ErrNotRecordBoundary) {
			t.Fatalf("before %d error = %v; want ErrNotRecordBoundary", before, err)
		}
	}
	var beyond *BeyondSizeError
	if _, _, err := scan(int64(len(content))+1, 5); !errors.As(err, &beyond) {
		t.Fatalf("before past size error = %v; want BeyondSizeError", err)
	}
}
