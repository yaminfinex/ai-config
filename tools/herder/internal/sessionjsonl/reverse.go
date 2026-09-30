// Package sessionjsonl provides shared low-level reads for append-only agent
// session files.
package sessionjsonl

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

const reverseBlockSize = 64 * 1024

// CompleteEnd returns the byte immediately after the last newline-terminated
// record. A trailing partial record is excluded without classifying the file.
func CompleteEnd(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return completeEnd(file, stat.Size(), make([]byte, reverseBlockSize))
}

// ErrNotRecordBoundary refuses a backward read whose offset is not the start
// of a complete record: it is neither 0 nor immediately after a newline at or
// before the complete-input end. Offsets a previous read produced always are.
var ErrNotRecordBoundary = errors.New("offset is not a complete record boundary")

// BeyondSizeError reports a backward read offset past the file's current
// size: the file was truncated or replaced since the offset was produced.
type BeyondSizeError struct {
	Offset int64
	Size   int64
}

func (e *BeyondSizeError) Error() string {
	return fmt.Sprintf("session offset %d beyond size %d", e.Offset, e.Size)
}

// ScanCompleteTail snapshots the current complete-input end, then visits
// complete records newest-first with stable zero-based line numbers and byte
// offsets. Line numbers come from a plain newline count of the prefix; no
// record is parsed outside the visited window. The reverse scan stops when
// visit returns false. Appends after the snapshot are left for the next read;
// truncation during the scan returns an error. reachedStart reports that no
// complete record remains before the last one visited.
func ScanCompleteTail(path string, visit func(raw []byte, line, offset int64) bool) (end int64, reachedStart bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return 0, false, err
	}
	buffer := make([]byte, reverseBlockSize)
	end, err = completeEnd(file, stat.Size(), buffer)
	if err != nil || end == 0 {
		return end, err == nil, err
	}
	reachedStart, err = scanBackward(file, end, buffer, visit)
	if err != nil {
		return 0, false, err
	}
	return end, reachedStart, nil
}

// ScanCompleteBefore visits the complete records that end at or before
// before, newest-first, with the same line numbers and offsets as
// ScanCompleteTail. before must be a record boundary a previous read
// produced: past the current size is a *BeyondSizeError, anything else that
// is not a complete record start is ErrNotRecordBoundary. Its cost is the
// newline count of the prefix plus the visited window.
func ScanCompleteBefore(path string, before int64, visit func(raw []byte, line, offset int64) bool) (reachedStart bool, err error) {
	if before < 0 {
		return false, ErrNotRecordBoundary
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return false, err
	}
	if before > stat.Size() {
		return false, &BeyondSizeError{Offset: before, Size: stat.Size()}
	}
	if before == 0 {
		return true, nil
	}
	buffer := make([]byte, reverseBlockSize)
	end, err := completeEnd(file, stat.Size(), buffer)
	if err != nil {
		return false, err
	}
	if before > end {
		return false, ErrNotRecordBoundary
	}
	last := []byte{0}
	if _, err := file.ReadAt(last, before-1); err != nil {
		return false, err
	}
	if last[0] != '\n' {
		return false, ErrNotRecordBoundary
	}
	return scanBackward(file, before, buffer, visit)
}

// scanBackward counts the lines before end, then visits the records ending at
// or before end newest-first until visit returns false.
func scanBackward(file *os.File, end int64, buffer []byte, visit func(raw []byte, line, offset int64) bool) (bool, error) {
	lines, err := countLines(file, end, buffer)
	if err != nil {
		return false, err
	}
	currentEnd := end
	line := lines - 1
	for currentEnd > 0 {
		lineEnd := currentEnd - 1
		newline, err := previousNewline(file, lineEnd, buffer)
		if err != nil {
			return false, err
		}
		lineStart := newline + 1
		raw := make([]byte, lineEnd-lineStart)
		if len(raw) > 0 {
			if _, err := file.ReadAt(raw, lineStart); err != nil {
				if errors.Is(err, io.EOF) {
					return false, io.ErrUnexpectedEOF
				}
				return false, err
			}
		}
		raw = bytes.TrimSuffix(raw, []byte{'\r'})
		if !visit(raw, line, lineStart) {
			return lineStart == 0, nil
		}
		line--
		currentEnd = lineStart
	}
	return true, nil
}

func countLines(file *os.File, end int64, buffer []byte) (int64, error) {
	var lines, offset int64
	for offset < end {
		count := int(min(int64(len(buffer)), end-offset))
		n, err := file.ReadAt(buffer[:count], offset)
		lines += int64(bytes.Count(buffer[:n], []byte{'\n'}))
		offset += int64(n)
		if errors.Is(err, io.EOF) {
			return 0, io.ErrUnexpectedEOF
		}
		if err != nil {
			return 0, err
		}
	}
	return lines, nil
}

// ScanCompleteReverse visits complete JSONL records newest-first and returns
// the complete-record end it scanned from, captured once before the first
// visit. A trailing partial record is ignored, matching the transcript
// readers' append contract. Scanning stops when visit returns false. Callers
// that seed an incremental tail continue from the returned end so bytes
// appended during the scan are never inside the offset unfolded.
func ScanCompleteReverse(path string, visit func([]byte) bool) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return 0, err
	}
	buffer := make([]byte, reverseBlockSize)
	completeEnd, err := completeEnd(file, stat.Size(), buffer)
	if err != nil || completeEnd == 0 {
		return 0, err
	}
	scannedEnd := completeEnd
	for completeEnd > 0 {
		lineEnd := completeEnd - 1
		newline, err := previousNewline(file, lineEnd, buffer)
		if err != nil {
			return scannedEnd, err
		}
		lineStart := newline + 1
		line := make([]byte, lineEnd-lineStart)
		if len(line) > 0 {
			if _, err := file.ReadAt(line, lineStart); err != nil && err != io.EOF {
				return scannedEnd, err
			}
		}
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if !visit(line) {
			return scannedEnd, nil
		}
		completeEnd = lineStart
	}
	return scannedEnd, nil
}

func completeEnd(file *os.File, size int64, buffer []byte) (int64, error) {
	if size == 0 {
		return 0, nil
	}
	last := []byte{0}
	if _, err := file.ReadAt(last, size-1); err != nil {
		return 0, err
	}
	if last[0] == '\n' {
		return size, nil
	}
	newline, err := previousNewline(file, size, buffer)
	if err != nil {
		return 0, err
	}
	return newline + 1, nil
}

func previousNewline(file *os.File, before int64, buffer []byte) (int64, error) {
	for before > 0 {
		start := max(int64(0), before-int64(len(buffer)))
		count := int(before - start)
		n, err := file.ReadAt(buffer[:count], start)
		if n < count {
			if err == nil || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return -1, err
		}
		if index := bytes.LastIndexByte(buffer[:count], '\n'); index >= 0 {
			return start + int64(index), nil
		}
		before = start
	}
	return -1, nil
}
