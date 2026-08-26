package claudesession

import (
	"errors"
	"os"
)

// Tail reads new complete entries or returns an explicit reset when the
// cursor no longer belongs to the current append-only file.
func Tail(path, sessionID string, cursor Cursor) (TailResult, error) {
	return tail(path, sessionID, cursor, 0)
}

// TailWindow applies Tail's reset semantics while capping the successful read
// to limit renderable entries.
func TailWindow(path, sessionID string, cursor Cursor, limit int) (TailResult, error) {
	if limit < 1 {
		return TailResult{}, os.ErrInvalid
	}
	return tail(path, sessionID, cursor, limit)
}

func tail(path, sessionID string, cursor Cursor, limit int) (TailResult, error) {
	if cursor.SessionID != "" && cursor.SessionID != sessionID {
		reset := &Reset{Reason: ResetSessionChanged, PreviousSessionID: cursor.SessionID, SessionID: sessionID, PreviousOffset: cursor.Offset}
		return TailResult{Cursor: Cursor{SessionID: sessionID}, Reset: reset}, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return TailResult{}, err
	}
	if st.Size() < cursor.Offset {
		reset := &Reset{Reason: ResetTruncated, PreviousSessionID: cursor.SessionID, SessionID: sessionID, PreviousOffset: cursor.Offset}
		return TailResult{Cursor: Cursor{SessionID: sessionID}, Reset: reset}, nil
	}
	var read ReadResult
	if limit > 0 {
		read, err = ReadWindow(path, cursor.Offset, limit)
	} else {
		read, err = ReadFrom(path, cursor.Offset)
	}
	if err != nil {
		if reset, ok := truncatedReadReset(err, sessionID, cursor); ok {
			return reset, nil
		}
		return TailResult{}, err
	}
	return TailResult{Read: read, Cursor: Cursor{SessionID: sessionID, Offset: read.NextOffset}}, nil
}

func truncatedReadReset(err error, sessionID string, cursor Cursor) (TailResult, bool) {
	var beyond *offsetBeyondError
	if !errors.As(err, &beyond) {
		return TailResult{}, false
	}
	reset := &Reset{Reason: ResetTruncated, PreviousSessionID: cursor.SessionID, SessionID: sessionID, PreviousOffset: cursor.Offset}
	return TailResult{Cursor: Cursor{SessionID: sessionID}, Reset: reset}, true
}
