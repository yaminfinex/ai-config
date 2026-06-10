package transcript

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
)

// NewSessionID returns a fresh random v4 UUID for use as a rewritten
// sessionId (and decant seed filename).
func NewSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// Rewrite streams a transcript, replacing the top-level sessionId value on
// every line that has one with newSessionID. All other bytes are preserved
// verbatim — key order, spacing, nested content, unknown fields. Lines
// without a sessionId (e.g. file-history-snapshot) pass through untouched.
// The parentUuid topology is by construction never modified.
func Rewrite(r io.Reader, w io.Writer, newSessionID string) error {
	bw := bufio.NewWriter(w)
	br := bufio.NewReaderSize(r, 64*1024)
	lineNum := 0
	for {
		line, err := br.ReadBytes('\n')
		hadNewline := bytes.HasSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) > 0 {
			lineNum++
			if len(bytes.TrimSpace(line)) > 0 {
				out, _, serr := spliceTopLevelString(line, "sessionId", newSessionID)
				if serr != nil {
					return fmt.Errorf("line %d: %w", lineNum, serr)
				}
				if _, werr := bw.Write(out); werr != nil {
					return werr
				}
				if hadNewline {
					if werr := bw.WriteByte('\n'); werr != nil {
						return werr
					}
				}
			}
		}
		if err == io.EOF {
			return bw.Flush()
		}
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNum+1, err)
		}
	}
}

// spliceTopLevelString replaces the value of a top-level string-valued key in
// a single JSON object line, preserving every other byte. Returns found=false
// (and the line unchanged) when the key is absent. The replacement value is
// JSON-encoded without HTML escaping, matching how Claude Code writes its
// own files.
func spliceTopLevelString(line []byte, key, value string) (out []byte, found bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		return nil, false, fmt.Errorf("invalid JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, false, fmt.Errorf("not a JSON object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, false, fmt.Errorf("invalid JSON: %w", err)
		}
		k, ok := keyTok.(string)
		if !ok {
			return nil, false, fmt.Errorf("invalid JSON object key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false, fmt.Errorf("invalid JSON: %w", err)
		}
		if k != key {
			continue
		}
		valEnd := dec.InputOffset()
		valStart := valEnd - int64(len(raw))
		enc, err := encodeJSONString(value)
		if err != nil {
			return nil, false, err
		}
		spliced := make([]byte, 0, len(line)-len(raw)+len(enc))
		spliced = append(spliced, line[:valStart]...)
		spliced = append(spliced, enc...)
		spliced = append(spliced, line[valEnd:]...)
		return spliced, true, nil
	}
	// Drain the closing brace so trailing garbage still errors.
	if _, err := dec.Token(); err != nil {
		return nil, false, fmt.Errorf("invalid JSON: %w", err)
	}
	return line, false, nil
}

// encodeJSONString marshals a string without HTML escaping (< > & stay
// literal, as in Claude Code's own output).
func encodeJSONString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
