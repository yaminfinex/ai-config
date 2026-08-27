package herdrcli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

// VisibleScreen is Herdr's ANSI-stripped plain-text projection of one pane.
// It never touches hcom's bidirectional PTY inject port.
type VisibleScreen struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Text        string `json:"text"`
	Revision    uint64 `json:"revision"`
	Truncated   bool   `json:"truncated"`
}

type LiveScreens struct{ socket string }

func NewLiveScreens() (*LiveScreens, error) {
	socket, err := liveSocket()
	if err != nil {
		return nil, err
	}
	return &LiveScreens{socket: socket}, nil
}

func (s *LiveScreens) ReadVisible(paneID string) (VisibleScreen, error) {
	conn, err := net.DialTimeout("unix", s.socket, 2*time.Second)
	if err != nil {
		return VisibleScreen{}, fmt.Errorf("connect herdr socket %s: %w", s.socket, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	req := map[string]any{"id": "herder-screen-read", "method": "pane.read", "params": map[string]any{"pane_id": paneID, "source": "visible", "format": "text", "strip_ansi": true}}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return VisibleScreen{}, fmt.Errorf("request herdr pane.read: %w", err)
	}
	decoder := json.NewDecoder(conn)
	for {
		var response struct {
			ID     any `json:"id"`
			Result struct {
				Read VisibleScreen `json:"read"`
			} `json:"result"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := decoder.Decode(&response); err != nil {
			if err == io.EOF {
				return VisibleScreen{}, fmt.Errorf("herdr socket closed before pane.read response")
			}
			return VisibleScreen{}, fmt.Errorf("read herdr pane.read: %w", err)
		}
		if fmt.Sprint(response.ID) != "herder-screen-read" {
			continue
		}
		if response.Error != nil {
			return VisibleScreen{}, fmt.Errorf("herdr pane.read: %s: %s", response.Error.Code, response.Error.Message)
		}
		if response.Result.Read.PaneID == "" {
			return VisibleScreen{}, fmt.Errorf("herdr pane.read returned no pane")
		}
		return response.Result.Read, nil
	}
}
