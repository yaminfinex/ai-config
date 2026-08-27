// Package sessionjsonl provides shared low-level reads for append-only agent
// session files.
package sessionjsonl

import (
	"bytes"
	"io"
	"os"
)

const reverseBlockSize = 64 * 1024

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
	completeEnd := stat.Size()
	if completeEnd == 0 {
		return nil
	}
	buffer := make([]byte, reverseBlockSize)
	last := []byte{0}
	if _, err := file.ReadAt(last, completeEnd-1); err != nil {
		return err
	}
	if last[0] != '\n' {
		newline, err := previousNewline(file, completeEnd, buffer)
		if err != nil || newline < 0 {
			return err
		}
		completeEnd = newline + 1
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
