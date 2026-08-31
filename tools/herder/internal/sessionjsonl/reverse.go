// Package sessionjsonl provides shared low-level reads for append-only agent
// session files.
package sessionjsonl

import (
	"bufio"
	"bytes"
	"errors"
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

// ScanCompleteTail snapshots the current complete-input end, inspects complete
// records oldest-first when inspect is non-nil, then visits them newest-first
// with stable zero-based line numbers and byte offsets. The reverse scan stops
// when visit returns false. Appends after the snapshot are left for the next
// read; truncation during the scan returns an error.
func ScanCompleteTail(path string, inspect func([]byte), visit func(raw []byte, line, offset int64) bool) (int64, error) {
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
	end, err := completeEnd(file, stat.Size(), buffer)
	if err != nil || end == 0 {
		return end, err
	}
	lines, err := inspectComplete(file, end, buffer, inspect)
	if err != nil {
		return 0, err
	}
	currentEnd := end
	line := lines - 1
	for currentEnd > 0 {
		lineEnd := currentEnd - 1
		newline, err := previousNewline(file, lineEnd, buffer)
		if err != nil {
			return 0, err
		}
		lineStart := newline + 1
		raw := make([]byte, lineEnd-lineStart)
		if len(raw) > 0 {
			if _, err := file.ReadAt(raw, lineStart); err != nil {
				if errors.Is(err, io.EOF) {
					return 0, io.ErrUnexpectedEOF
				}
				return 0, err
			}
		}
		raw = bytes.TrimSuffix(raw, []byte{'\r'})
		if !visit(raw, line, lineStart) {
			break
		}
		line--
		currentEnd = lineStart
	}
	return end, nil
}

func inspectComplete(file *os.File, end int64, buffer []byte, inspect func([]byte)) (int64, error) {
	if inspect == nil {
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
	reader := bufio.NewReader(io.NewSectionReader(file, 0, end))
	var lines int64
	for {
		raw, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) && len(raw) == 0 {
			return lines, nil
		}
		if err != nil {
			return 0, err
		}
		body := bytes.TrimSuffix(raw[:len(raw)-1], []byte{'\r'})
		inspect(body)
		lines++
	}
}

// ScanCompleteReverse visits complete JSONL records newest-first. A trailing
// partial record is ignored, matching the transcript readers' append contract.
// Scanning stops when visit returns false.
func ScanCompleteReverse(path string, visit func([]byte) bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	buffer := make([]byte, reverseBlockSize)
	completeEnd, err := completeEnd(file, stat.Size(), buffer)
	if err != nil || completeEnd == 0 {
		return err
	}
	for completeEnd > 0 {
		lineEnd := completeEnd - 1
		newline, err := previousNewline(file, lineEnd, buffer)
		if err != nil {
			return err
		}
		lineStart := newline + 1
		line := make([]byte, lineEnd-lineStart)
		if len(line) > 0 {
			if _, err := file.ReadAt(line, lineStart); err != nil && err != io.EOF {
				return err
			}
		}
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if !visit(line) {
			return nil
		}
		completeEnd = lineStart
	}
	return nil
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
		if _, err := file.ReadAt(buffer[:count], start); err != nil && err != io.EOF {
			return -1, err
		}
		if index := bytes.LastIndexByte(buffer[:count], '\n'); index >= 0 {
			return start + int64(index), nil
		}
		before = start
	}
	return -1, nil
}
