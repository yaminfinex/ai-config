package herdrcli

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
)

func TestParseSessionSnapshotResult(t *testing.T) {
	wrapped := []byte(`{"type":"session_snapshot","snapshot":{"protocol":19,"version":"fixture","panes":[{"pane_id":"p1","agent_session":{"value":"s1"}}],"agents":[{"pane_id":"p1","name":"mavu"}]}}`)
	snapshot, err := ParseSessionSnapshotResult(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Protocol != 19 || len(snapshot.Panes) != 1 || snapshot.Panes[0].AgentSession != "s1" {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	direct := []byte(`{"protocol":19,"panes":[{"pane_id":"p2","agent_session":"s2"}],"agents":[]}`)
	snapshot, err = ParseSessionSnapshotResult(direct)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Panes[0].AgentSession != "s2" {
		t.Fatalf("direct snapshot = %#v", snapshot)
	}
}

func TestParseSessionSnapshotResultRejectsEmpty(t *testing.T) {
	for _, input := range []string{`{}`, `{"snapshot":{}}`, `null`} {
		if _, err := ParseSessionSnapshotResult([]byte(input)); err == nil {
			t.Errorf("ParseSessionSnapshotResult(%s) succeeded", input)
		}
	}
}

func TestParseServerStatusJSONAndText(t *testing.T) {
	for name, input := range map[string]string{
		"json": `{"result":{"socket":"/tmp/herdr.sock","protocol":19,"compatible":true}}`,
		"text": "socket: /tmp/herdr.sock\nprotocol: 19\ncompatible: yes\n",
	} {
		t.Run(name, func(t *testing.T) {
			status, err := parseServerStatus([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			if status.socket != "/tmp/herdr.sock" || status.protocol != 19 || !status.compatible {
				t.Fatalf("status = %#v", status)
			}
		})
	}

	t.Run("socket only", func(t *testing.T) {
		status, err := parseServerStatus([]byte(`{"result":{"socket":"/tmp/herdr.sock"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if status.socket != "/tmp/herdr.sock" || status.protocol != 0 || status.compatible {
			t.Fatalf("status = %#v", status)
		}
	})
}

func TestSupportsServerProtocol(t *testing.T) {
	for _, test := range []struct {
		name       string
		protocol   int
		compatible bool
		want       bool
	}{
		{name: "current compatible", protocol: 19, compatible: true, want: true},
		{name: "newer compatible", protocol: 20, compatible: true, want: true},
		{name: "newer incompatible", protocol: 20, compatible: false, want: false},
		{name: "older compatible", protocol: 18, compatible: true, want: false},
		{name: "absent status fields", protocol: 0, compatible: false, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := supportsServerProtocol(serverStatus{protocol: test.protocol, compatible: test.compatible})
			if got != test.want {
				t.Fatalf("supportsServerProtocol(protocol=%d, compatible=%t) = %t, want %t", test.protocol, test.compatible, got, test.want)
			}
		})
	}
}

func TestPaneRejectsMalformedAgentSession(t *testing.T) {
	var pane Pane
	if err := json.Unmarshal([]byte(`{"pane_id":"p1","agent_session":[]}`), &pane); err == nil {
		t.Fatal("Pane accepted malformed agent_session")
	}
}

func TestLiveSnapshotReadsFixtureFromFakeUnixSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer connection.Close()
		var request map[string]any
		if err := json.NewDecoder(bufio.NewReader(connection)).Decode(&request); err != nil {
			done <- err
			return
		}
		response := map[string]any{
			"id": request["id"],
			"result": map[string]any{"snapshot": map[string]any{
				"protocol":   19,
				"version":    "fixture",
				"workspaces": []map[string]any{{"workspace_id": "w1"}},
				"tabs":       []map[string]any{{"tab_id": "t1", "workspace_id": "w1"}},
				"panes":      []map[string]any{{"pane_id": "p1", "workspace_id": "w1", "tab_id": "t1"}},
				"agents":     []map[string]any{{"pane_id": "p1", "name": "dore"}},
			}},
		}
		done <- json.NewEncoder(connection).Encode(response)
	}()
	t.Setenv("HERDER_HERDR_SOCKET", socket)
	snapshot, err := LiveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Workspaces) != 1 || len(snapshot.Tabs) != 1 || len(snapshot.Panes) != 1 || snapshot.Agents[0].Name != "dore" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
