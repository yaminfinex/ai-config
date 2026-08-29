package herdrcli

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"testing"
)

func TestSendInputForwardsTextAndNamedKeysWithoutTranslation(t *testing.T) {
	for name, input := range map[string]PaneInput{
		"raw text":   {Text: "\x03\x1b[A\x1b[200~paste\x1b[201~"},
		"named keys": {Keys: []string{"ctrl+c", "up", "escape"}},
	} {
		t.Run(name, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "herdr.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					done <- acceptErr
					return
				}
				defer connection.Close()
				var request struct {
					ID, Method string
					Params     PaneInput `json:"params"`
				}
				if err := json.NewDecoder(connection).Decode(&request); err != nil {
					done <- err
					return
				}
				if request.Method != "pane.send_input" || request.Params.PaneID != "w1:p2" || request.Params.Text != input.Text || fmt.Sprint(request.Params.Keys) != fmt.Sprint(input.Keys) {
					done <- fmt.Errorf("unexpected pane.send_input request: %#v", request)
					return
				}
				done <- json.NewEncoder(connection).Encode(map[string]any{"id": request.ID, "result": map[string]any{"ok": true}})
			}()
			input.PaneID = "w1:p2"
			if err := (&LiveScreens{socket: socket}).SendInput(input); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReadVisibleUsesPinnedReadOnlyPaneProtocol(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer connection.Close()
		var request struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				PaneID    string `json:"pane_id"`
				Source    string `json:"source"`
				Format    string `json:"format"`
				StripANSI bool   `json:"strip_ansi"`
			} `json:"params"`
		}
		if decodeErr := json.NewDecoder(connection).Decode(&request); decodeErr != nil {
			done <- decodeErr
			return
		}
		if request.ID != "herder-screen-read" || request.Method != "pane.read" || request.Params.PaneID != "w1:p2" || request.Params.Source != "visible" || request.Params.Format != "ansi" || request.Params.StripANSI {
			done <- fmt.Errorf("unexpected pane.read request: %#v", request)
			return
		}
		done <- json.NewEncoder(connection).Encode(map[string]any{
			"id": request.ID,
			"result": map[string]any{"read": map[string]any{
				"pane_id": "w1:p2", "workspace_id": "w1", "tab_id": "w1:t1", "text": "\u001b[31mcolored terminal\u001b[0m", "revision": 42, "truncated": false,
			}},
		})
	}()

	reader := &LiveScreens{socket: socket}
	screen, err := reader.ReadVisible("w1:p2")
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if screen.PaneID != "w1:p2" || screen.Text != "\x1b[31mcolored terminal\x1b[0m" || screen.Revision != 42 {
		t.Fatalf("screen = %#v", screen)
	}
}

func TestReadHistoryUsesBoundedRecentANSIProtocol(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer connection.Close()
		var request struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				PaneID    string `json:"pane_id"`
				Source    string `json:"source"`
				Format    string `json:"format"`
				StripANSI bool   `json:"strip_ansi"`
				Lines     int    `json:"lines"`
			} `json:"params"`
		}
		if decodeErr := json.NewDecoder(connection).Decode(&request); decodeErr != nil {
			done <- decodeErr
			return
		}
		if request.ID != "herder-screen-history" || request.Method != "pane.read" || request.Params.PaneID != "w1:p2" || request.Params.Source != "recent" || request.Params.Format != "ansi" || request.Params.StripANSI || request.Params.Lines != 2000 {
			done <- fmt.Errorf("unexpected pane.read request: %#v", request)
			return
		}
		done <- json.NewEncoder(connection).Encode(map[string]any{
			"id": request.ID,
			"result": map[string]any{"read": map[string]any{
				"pane_id": "w1:p2", "workspace_id": "w1", "tab_id": "w1:t1", "text": "history", "revision": 43, "truncated": true,
			}},
		})
	}()

	reader := &LiveScreens{socket: socket}
	history, err := reader.ReadHistory("w1:p2", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if history.PaneID != "w1:p2" || history.Text != "history" || !history.Truncated {
		t.Fatalf("history = %#v", history)
	}
}
