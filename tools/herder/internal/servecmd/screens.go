package servecmd

import (
	"encoding/json"
	"fmt"

	"ai-config/tools/herder/internal/herdrcli"
)

const (
	maxScreenFrameBytes = 16_384
	screenThrottleNanos = 250_000_000
)

type screenSource interface {
	ReadVisible(string) (herdrcli.VisibleScreen, error)
}

type screenFrame struct {
	PaneID    string `json:"pane_id"`
	Revision  uint64 `json:"revision,omitempty"`
	Status    string `json:"status"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Detail    string `json:"detail,omitempty"`
}

type eventScreen struct {
	paneID     string
	revision   uint64
	text       string
	truncated  bool
	status     string
	detail     string
	dirty      bool
	visible    bool
	lastEmitNS int64
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
	for _, field := range []*string{&frame.Text, &frame.Detail} {
		original := []rune(*field)
		lo, hi := 0, len(original)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			*field = string(original[:mid])
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
		*field = string(original[:lo])
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

func paneRevisions(snapshot herdrcli.Snapshot) map[string]uint64 {
	result := make(map[string]uint64, len(snapshot.Panes))
	for _, pane := range snapshot.Panes {
		result[pane.PaneID] = pane.Revision
	}
	return result
}
