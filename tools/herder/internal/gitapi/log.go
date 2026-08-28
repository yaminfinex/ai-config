package gitapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxCursorSkip = 1_000_000

type logCursor struct {
	Version int    `json:"v"`
	Anchor  string `json:"anchor"`
	Skip    int    `json:"skip"`
	Bind    string `json:"bind"`
}

func ReadLog(ctx context.Context, root, requestedPath, cursor string, now func() time.Time) (LogResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	loc, err := locatePath(ctx, root, requestedPath)
	if err != nil {
		return LogResult{}, err
	}
	anchor, skip, err := logPosition(ctx, loc, cursor)
	if err != nil {
		return LogResult{}, err
	}
	format := "%H%x00%an%x00%aI%x00%s"
	out, err := gitOutput(ctx, loc.repoTop, "log", "--follow", "--format="+format, "-z", "--max-count="+strconv.Itoa(LogPageSize+1), "--skip="+strconv.Itoa(skip), anchor, "--", loc.repoPath)
	if err != nil {
		return LogResult{}, err
	}
	entries, err := parseLog(out)
	if err != nil {
		return LogResult{}, err
	}
	if len(entries) == 0 {
		if _, statErr := os.Lstat(filepath.Join(loc.root, filepath.FromSlash(loc.path))); os.IsNotExist(statErr) {
			return LogResult{}, fmt.Errorf("%w: path %q has no current file or history", ErrNotFound, requestedPath)
		}
	}
	result := LogResult{Root: loc.root, Path: loc.path, Entries: entries, FetchedAt: now()}
	if len(result.Entries) > LogPageSize {
		result.Entries = result.Entries[:LogPageSize]
		result.NextCursor, err = encodeCursor(logCursor{Version: 1, Anchor: anchor, Skip: skip + LogPageSize, Bind: cursorBinding(loc)})
		if err != nil {
			return LogResult{}, err
		}
	}
	return result, nil
}

func logPosition(ctx context.Context, loc location, encoded string) (string, int, error) {
	if encoded == "" {
		out, err := gitOutput(ctx, loc.repoTop, "rev-parse", "--verify", "--end-of-options", "HEAD^{commit}")
		if err != nil {
			return "", 0, fmt.Errorf("%w: HEAD does not resolve to a commit", ErrUnavailable)
		}
		return strings.TrimSpace(string(out)), 0, nil
	}
	cursor, err := decodeCursor(encoded)
	if err != nil {
		return "", 0, err
	}
	if cursor.Bind != cursorBinding(loc) {
		return "", 0, fmt.Errorf("%w: cursor does not belong to this root and path", ErrRefused)
	}
	return cursor.Anchor, cursor.Skip, nil
}

func parseLog(output []byte) ([]LogEntry, error) {
	parts := bytes.Split(output, []byte{0})
	for len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	if len(parts)%4 != 0 {
		return nil, fmt.Errorf("git log returned %d NUL fields, want a multiple of four", len(parts))
	}
	entries := make([]LogEntry, 0, len(parts)/4)
	for index := 0; index < len(parts); index += 4 {
		entry := LogEntry{SHA: string(parts[index]), Author: string(parts[index+1]), Date: string(parts[index+2]), Subject: string(parts[index+3])}
		if !fullSHA(entry.SHA) {
			return nil, fmt.Errorf("git log returned invalid commit id %q", entry.SHA)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func cursorBinding(loc location) string {
	sum := sha256.Sum256([]byte(loc.repoTop + "\x00" + loc.rootPrefix + "\x00" + loc.path))
	return hex.EncodeToString(sum[:])
}

func encodeCursor(cursor logCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode log cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeCursor(value string) (logCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) > 1024 {
		return logCursor{}, fmt.Errorf("%w: malformed log cursor", ErrBadRequest)
	}
	var cursor logCursor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || !fullSHA(cursor.Anchor) || cursor.Skip < 0 || cursor.Skip > maxCursorSkip || len(cursor.Bind) != 64 {
		return logCursor{}, fmt.Errorf("%w: malformed log cursor", ErrBadRequest)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return logCursor{}, fmt.Errorf("%w: malformed log cursor", ErrBadRequest)
	}
	return cursor, nil
}
