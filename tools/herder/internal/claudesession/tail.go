package claudesession

import "os"

// Tail reads new complete entries or returns an explicit reset when the
// cursor no longer belongs to the current append-only file.
func Tail(path, sessionID string, cursor Cursor) (TailResult, error) {
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
	read, err := ReadFrom(path, cursor.Offset)
	if err != nil {
		return TailResult{}, err
	}
	return TailResult{Read: read, Cursor: Cursor{SessionID: sessionID, Offset: read.NextOffset}}, nil
}
