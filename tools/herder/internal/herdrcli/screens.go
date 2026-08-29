package herdrcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

var ErrPaneGone = errors.New("pane disappeared before input was accepted")

type PaneInput struct {
	PaneID string   `json:"pane_id"`
	Text   string   `json:"text,omitempty"`
	Keys   []string `json:"keys,omitempty"`
}

// VisibleScreen is Herdr's ANSI projection of one pane.
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
	return s.read(paneID, "herder-screen-read", "visible", 0)
}

func (s *LiveScreens) ReadHistory(paneID string, lines int) (VisibleScreen, error) {
	return s.read(paneID, "herder-screen-history", "recent", lines)
}

func (s *LiveScreens) SendInput(input PaneInput) error {
	conn, err := net.DialTimeout("unix", s.socket, 2*time.Second)
	if err != nil {
		return fmt.Errorf("connect herdr socket %s: %w", s.socket, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	const requestID = "herder-pane-input"
	request := map[string]any{"id": requestID, "method": "pane.send_input", "params": input}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return fmt.Errorf("request herdr pane.send_input: %w", err)
	}
	decoder := json.NewDecoder(conn)
	for {
		var response struct {
			ID    any                             `json:"id"`
			Error *struct{ Code, Message string } `json:"error"`
		}
		if err := decoder.Decode(&response); err != nil {
			if err == io.EOF {
				return fmt.Errorf("herdr socket closed before pane.send_input response")
			}
			return fmt.Errorf("read herdr pane.send_input: %w", err)
		}
		if fmt.Sprint(response.ID) != requestID {
			continue
		}
		if response.Error != nil {
			lower := strings.ToLower(response.Error.Code + " " + response.Error.Message)
			if strings.Contains(lower, "pane") && (strings.Contains(lower, "not found") || strings.Contains(lower, "gone") || strings.Contains(lower, "missing")) {
				return fmt.Errorf("%w: %s: %s", ErrPaneGone, response.Error.Code, response.Error.Message)
			}
			return fmt.Errorf("herdr pane.send_input: %s: %s", response.Error.Code, response.Error.Message)
		}
		return nil
	}
}

func (s *LiveScreens) read(paneID, requestID, source string, lines int) (VisibleScreen, error) {
	conn, err := net.DialTimeout("unix", s.socket, 2*time.Second)
	if err != nil {
		return VisibleScreen{}, fmt.Errorf("connect herdr socket %s: %w", s.socket, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	params := map[string]any{"pane_id": paneID, "source": source, "format": "ansi", "strip_ansi": false}
	if lines > 0 {
		params["lines"] = lines
	}
	req := map[string]any{"id": requestID, "method": "pane.read", "params": params}
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
		if fmt.Sprint(response.ID) != requestID {
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
