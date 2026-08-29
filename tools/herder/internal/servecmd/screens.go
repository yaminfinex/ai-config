package servecmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-config/tools/herder/internal/herdrcli"
)

const (
	maxScreenFrameBytes     = 64 << 10
	backgroundScreenCadence = 250 * time.Millisecond
	focusedScreenCadence    = 100 * time.Millisecond
	maxHistoryLines         = 2000
)

type screenSource interface {
	ReadVisible(string) (herdrcli.VisibleScreen, error)
	ReadHistory(string, int) (herdrcli.VisibleScreen, error)
	SendInput(herdrcli.PaneInput) error
}

type screenFrame struct {
	PaneID    string `json:"pane_id"`
	Revision  uint64 `json:"revision,omitempty"`
	Status    string `json:"status"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Detail    string `json:"detail,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
}

type eventScreen struct {
	paneID      string
	revision    uint64
	text        string
	truncated   bool
	status      string
	detail      string
	cols        int
	rows        int
	emittedCols int
	emittedRows int
	dirty       bool
	visible     bool
	lastPollNS  int64
}

type screenPaneFact struct {
	revision uint64
	cols     int
	rows     int
}

type paneHistory struct {
	PaneID    string `json:"pane_id"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	FetchedAt string `json:"fetched_at"`
}

func encodeScreenEvent(paneID string, frame screenFrame) ([]byte, error) {
	eventType := "screen:" + paneID
	encode := func() ([]byte, error) {
		data, err := json.Marshal(frame)
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)), nil
	}
	wire, err := encode()
	if err != nil {
		return nil, err
	}
	if len(wire) <= maxScreenFrameBytes {
		return wire, nil
	}
	frame.Truncated = true
	originalText := frame.Text
	boundaries := ansiLineBoundaries(originalText)
	frame.Text = ""
	lo, hi := 0, len(boundaries)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		frame.Text = originalText[:boundaries[mid-1]]
		candidate, marshalErr := encode()
		if marshalErr != nil {
			return nil, marshalErr
		}
		if len(candidate) <= maxScreenFrameBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo > 0 {
		frame.Text = originalText[:boundaries[lo-1]]
	} else {
		frame.Text = ""
	}
	wire, err = encode()
	if err != nil {
		return nil, err
	}
	if len(wire) <= maxScreenFrameBytes {
		return wire, nil
	}
	if frame.Detail != "" {
		original := []rune(frame.Detail)
		lo, hi := 0, len(original)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			frame.Detail = string(original[:mid])
			candidate, marshalErr := encode()
			if marshalErr != nil {
				return nil, marshalErr
			}
			if len(candidate) <= maxScreenFrameBytes {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		frame.Detail = string(original[:lo])
		wire, err = encode()
		if err != nil {
			return nil, err
		}
		if len(wire) <= maxScreenFrameBytes {
			return wire, nil
		}
	}
	return nil, fmt.Errorf("screen frame metadata exceeds %d-byte budget", maxScreenFrameBytes)
}

func ansiLineBoundaries(text string) []int {
	const (
		ground = iota
		escape
		csi
		stringEscape
		stringEscapeEnd
	)
	state := ground
	boundaries := make([]int, 0, strings.Count(text, "\n"))
	for index := 0; index < len(text); index++ {
		value := text[index]
		switch state {
		case ground:
			if value == 0x1b {
				state = escape
			}
		case escape:
			switch value {
			case '[':
				state = csi
			case ']', 'P', 'X', '^', '_':
				state = stringEscape
			default:
				state = ground
			}
		case csi:
			if value >= 0x40 && value <= 0x7e {
				state = ground
			}
		case stringEscape:
			if value == 0x07 {
				state = ground
			} else if value == 0x1b {
				state = stringEscapeEnd
			}
		case stringEscapeEnd:
			if value == '\\' {
				state = ground
			} else if value != 0x1b {
				state = stringEscape
			}
		}
		if value == '\n' && state == ground {
			boundaries = append(boundaries, index+1)
		}
	}
	return boundaries
}

func paneScreenFacts(snapshot herdrcli.Snapshot) map[string]screenPaneFact {
	result := make(map[string]screenPaneFact, len(snapshot.Panes))
	for _, pane := range snapshot.Panes {
		cols, rows, _ := snapshot.PaneSize(pane.PaneID)
		result[pane.PaneID] = screenPaneFact{revision: pane.Revision, cols: cols, rows: rows}
	}
	return result
}
