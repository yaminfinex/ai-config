package servecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"ai-config/tools/herder/internal/claudesession"
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
	path, err := claudesession.Resolve(home, row)
	if err != nil {
		var resolveErr *claudesession.ResolveError
		if errors.As(err, &resolveErr) {
			refuse(w, http.StatusConflict, "no session", err.Error())
			return
		}
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}

	window := entriesWindow{Mode: "from", From: from, Limit: limit}
	var read claudesession.ReadResult
	if hasFrom {
		tail, tailErr := claudesession.TailWindow(path, row.SessionID, claudesession.Cursor{SessionID: previousSessionID, Offset: from}, limit)
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
		read, window.From, err = claudesession.ReadTail(path, limit)
		if err != nil {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
			return
		}
	}

	entries := make([]entryResponse, len(read.Entries))
	for i, entry := range read.Entries {
		entries[i] = entryResponse{
			UUID: entry.UUID, Line: entry.Line, ByteOffset: entry.ByteOffset,
			Timestamp: entry.Timestamp, Kind: entry.Kind, Payload: entry.Payload,
			Quarantine: entry.Quarantine,
		}
	}
	next := read.NextOffset
	stats := entriesStats{SidechainSkipped: read.Stats.SidechainSkipped}
	writeJSON(w, http.StatusOK, entriesResponse{
		SessionID: row.SessionID, Window: window, Entries: &entries,
		NextOffset: &next, Stats: &stats,
	})
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
