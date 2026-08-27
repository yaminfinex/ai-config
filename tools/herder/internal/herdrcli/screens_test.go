package herdrcli

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"testing"
)

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
		if request.ID != "herder-screen-read" || request.Method != "pane.read" || request.Params.PaneID != "w1:p2" || request.Params.Source != "visible" || request.Params.Format != "text" || !request.Params.StripANSI {
			done <- fmt.Errorf("unexpected pane.read request: %#v", request)
			return
		}
		done <- json.NewEncoder(connection).Encode(map[string]any{
			"id": request.ID,
			"result": map[string]any{"read": map[string]any{
				"pane_id": "w1:p2", "workspace_id": "w1", "tab_id": "w1:t1", "text": "plain terminal", "revision": 42, "truncated": false,
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
	if screen.PaneID != "w1:p2" || screen.Text != "plain terminal" || screen.Revision != 42 {
		t.Fatalf("screen = %#v", screen)
	}
}
