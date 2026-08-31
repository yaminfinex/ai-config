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
	err := ScanCompleteReverse(path, func(line []byte) bool {
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
	var inspected []string
	type record struct {
		body   string
		line   int64
		offset int64
	}
	var records []record
	end, err := ScanCompleteTail(path, func(raw []byte) {
		inspected = append(inspected, string(raw))
	}, func(raw []byte, line, offset int64) bool {
		records = append(records, record{body: string(raw), line: line, offset: offset})
		return true
	})
	wantRecords := []record{
		{body: "invented-two", line: 1, offset: int64(len("invented-one\r\n"))},
		{body: "invented-one", line: 0, offset: 0},
	}
	if err != nil || end != int64(len("invented-one\r\ninvented-two\n")) {
		t.Fatalf("tail scan end = %d, %v", end, err)
	}
	if !reflect.DeepEqual(inspected, []string{"invented-one", "invented-two"}) || !reflect.DeepEqual(records, wantRecords) {
		t.Fatalf("tail scan inspected=%#v records=%#v", inspected, records)
	}
}

func TestScanCompleteTailFailsClosedWhenSnapshotIsTruncated(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "invented.jsonl")
	if err := os.WriteFile(path, []byte("invented-one\ninvented-two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	truncated := false
	_, err := ScanCompleteTail(path, func([]byte) {
		if truncated {
			return
		}
		truncated = true
		if truncateErr := os.Truncate(path, 0); truncateErr != nil {
			t.Fatal(truncateErr)
		}
	}, func([]byte, int64, int64) bool { return true })
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("tail scan error = %v; want unexpected EOF", err)
	}
}
