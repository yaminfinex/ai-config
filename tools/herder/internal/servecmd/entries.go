package servecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/codexsession"
	"ai-config/tools/herder/internal/hcomidentity"
)

const maxEntryWindow = 500

type entriesWindow struct {
	Mode  string `json:"mode"`
	From  int64  `json:"from"`
	Limit int    `json:"limit"`
}

type entriesStats struct {
	SidechainSkipped int `json:"sidechainSkipped"`
}

type entryResponse struct {
	UUID       string                    `json:"uuid,omitempty"`
	Line       int64                     `json:"line"`
	ByteOffset int64                     `json:"byteOffset"`
	Timestamp  string                    `json:"timestamp,omitempty"`
	Kind       claudesession.Kind        `json:"kind"`
	Payload    json.RawMessage           `json:"payload"`
	Quarantine *claudesession.Quarantine `json:"quarantine,omitempty"`
}

type entriesResponse struct {
	SessionID  string               `json:"sessionId"`
	Window     entriesWindow        `json:"window"`
	Entries    *[]entryResponse     `json:"entries,omitempty"`
	NextOffset *int64               `json:"nextOffset,omitempty"`
	Reset      *claudesession.Reset `json:"reset,omitempty"`
	Stats      *entriesStats        `json:"stats,omitempty"`
}

func serveEntries(w http.ResponseWriter, r *http.Request, deps dependencies, name string) {
	limit, err := entryLimit(r)
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	from, hasFrom, err := entryOffset(r)
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	previousSessionID := r.URL.Query().Get("sessionId")
	if !hasFrom && previousSessionID != "" {
		refuse(w, http.StatusBadRequest, "bad request", "sessionId requires from")
		return
	}

	row, err := entryAgent(deps, name)
	if err != nil {
		serveAgentReadError(w, err)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	path, err := resolveEntryPath(home, row)
	if err != nil {
		var resolveErr *claudesession.ResolveError
		var codexResolveErr *codexsession.ResolveError
		if errors.As(err, &resolveErr) || errors.As(err, &codexResolveErr) {
			refuse(w, http.StatusConflict, "no session", err.Error())
			return
		}
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}

	window := entriesWindow{Mode: "from", From: from, Limit: limit}
	var read claudesession.ReadResult
	if hasFrom {
		tail, tailErr := readEntryWindow(path, row, claudesession.Cursor{SessionID: previousSessionID, Offset: from}, limit)
		if tailErr != nil {
			refuse(w, http.StatusBadGateway, "substrate unreachable", tailErr.Error())
			return
		}
		if tail.Reset != nil {
			writeJSON(w, http.StatusOK, entriesResponse{SessionID: row.SessionID, Window: window, Reset: tail.Reset})
			return
		}
		read = tail.Read
	} else {
		window.Mode = "tail"
		read, window.From, err = readEntryTail(path, row, limit)
		if err != nil {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
			return
		}
	}

	entries := serializeEntries(read.Entries)
	next := read.NextOffset
	stats := entriesStats{SidechainSkipped: read.Stats.SidechainSkipped}
	writeJSON(w, http.StatusOK, entriesResponse{
		SessionID: row.SessionID, Window: window, Entries: &entries,
		NextOffset: &next, Stats: &stats,
	})
}

// serializeEntries is the single wire projection shared by entry windows and
// multiplexed entry frames. Those two surfaces must never drift in shape.
func serializeEntries(entries []claudesession.Entry) []entryResponse {
	serialized := make([]entryResponse, len(entries))
	for i, entry := range entries {
		serialized[i] = entryResponse{
			UUID: entry.UUID, Line: entry.Line, ByteOffset: entry.ByteOffset,
			Timestamp: entry.Timestamp, Kind: entry.Kind, Payload: entry.Payload,
			Quarantine: entry.Quarantine,
		}
	}
	return serialized
}

func entryLimit(r *http.Request) (int, error) {
	limit := maxEntryWindow
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxEntryWindow {
			return 0, fmt.Errorf("limit must be an integer from 1 through %d", maxEntryWindow)
		}
		limit = parsed
	}
	return limit, nil
}

func entryOffset(r *http.Request) (int64, bool, error) {
	raw, present := r.URL.Query()["from"]
	if !present {
		return 0, false, nil
	}
	if len(raw) != 1 {
		return 0, false, errors.New("from must be one non-negative byte offset")
	}
	offset, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil || offset < 0 {
		return 0, false, errors.New("from must be one non-negative byte offset")
	}
	return offset, true, nil
}

func entryAgent(deps dependencies, name string) (hcomidentity.Row, error) {
	roster, err := deps.roster()
	if err != nil {
		return hcomidentity.Row{}, sourceError{"hcom", err}
	}
	for _, row := range roster {
		if row.Name == name {
			return row, nil
		}
	}
	return hcomidentity.Row{}, fmt.Errorf("%w: agent %q is not on the hcom bus", errUnknownAgent, name)
}

func entryTailEnd(row hcomidentity.Row) (int64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	path, err := resolveEntryPath(home, row)
	if err != nil {
		return 0, err
	}
	var read claudesession.ReadResult
	if row.Tool == "codex" {
		read, _, err = codexsession.ReadTail(path, 1)
	} else {
		read, _, err = claudesession.ReadTail(path, 1)
	}
	return read.NextOffset, err
}

func entryTail(row hcomidentity.Row, cursor claudesession.Cursor, limit int) (claudesession.TailResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return claudesession.TailResult{}, err
	}
	path, err := resolveEntryPath(home, row)
	if err != nil {
		return claudesession.TailResult{}, err
	}
	if row.Tool == "codex" {
		return codexsession.TailWindow(path, row.SessionID, cursor, limit)
	}
	return claudesession.TailWindow(path, row.SessionID, cursor, limit)
}

func readAgentVitals(row hcomidentity.Row) (claudesession.Vitals, error) {
	if row.Tool != "claude" && row.Tool != "codex" {
		return claudesession.Vitals{}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return claudesession.Vitals{}, err
	}
	path, err := resolveEntryPath(home, row)
	if err != nil {
		var claudeResolve *claudesession.ResolveError
		var codexResolve *codexsession.ResolveError
		if errors.As(err, &claudeResolve) || errors.As(err, &codexResolve) {
			return claudesession.Vitals{}, nil
		}
		return claudesession.Vitals{}, err
	}
	if row.Tool == "codex" {
		return codexsession.ReadVitals(path)
	}
	return claudesession.ReadVitals(path)
}

// readDeliveredMessageIDs scans normalized session windows without retaining
// the full transcript. It stops as soon as every recent candidate is proven.
func readDeliveredMessageIDs(row hcomidentity.Row, candidates map[string]struct{}) (map[string]bool, error) {
	delivered := make(map[string]bool)
	if len(candidates) == 0 {
		return delivered, nil
	}
	if row.Tool != "claude" && row.Tool != "codex" {
		return nil, fmt.Errorf("unsupported session tool %q", row.Tool)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path, err := resolveEntryPath(home, row)
	if err != nil {
		return nil, err
	}
	var offset int64
	for {
		var read claudesession.ReadResult
		if row.Tool == "codex" {
			read, err = codexsession.ReadWindow(path, offset, maxEntryWindow)
		} else {
			read, err = claudesession.ReadWindow(path, offset, maxEntryWindow)
		}
		if err != nil {
			return nil, err
		}
		for id := range deliveredMessageIDs(read.Entries) {
			if _, wanted := candidates[id]; wanted {
				delivered[id] = true
			}
		}
		if len(delivered) == len(candidates) || read.NextOffset == offset {
			return delivered, nil
		}
		offset = read.NextOffset
	}
}

func resolveEntryPath(home string, row hcomidentity.Row) (string, error) {
	switch row.Tool {
	case "claude":
		return claudesession.Resolve(home, row)
	case "codex":
		return codexsession.Resolve(home, row)
	default:
		// Preserve the existing non-file-tool refusal category.
		return claudesession.Resolve(home, row)
	}
}

func readEntryWindow(path string, row hcomidentity.Row, cursor claudesession.Cursor, limit int) (claudesession.TailResult, error) {
	if row.Tool == "codex" {
		return codexsession.TailWindow(path, row.SessionID, cursor, limit)
	}
	return claudesession.TailWindow(path, row.SessionID, cursor, limit)
}

func readEntryTail(path string, row hcomidentity.Row, limit int) (claudesession.ReadResult, int64, error) {
	if row.Tool == "codex" {
		return codexsession.ReadTail(path, limit)
	}
	return claudesession.ReadTail(path, limit)
}
