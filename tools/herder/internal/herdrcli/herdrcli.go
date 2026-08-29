// Package herdrcli reads and decodes the live herdr session snapshot.
package herdrcli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const supportedProtocol = 19

type Agent struct {
	PaneID string `json:"pane_id"`
	Agent  string `json:"agent"`
	Status string `json:"agent_status"`
	Name   string `json:"name"`
}

type Pane struct {
	PaneID         string `json:"pane_id"`
	WorkspaceID    string `json:"workspace_id"`
	TabID          string `json:"tab_id"`
	Label          string `json:"label"`
	Agent          string `json:"agent"`
	AgentStatus    string `json:"agent_status"`
	AgentSession   string `json:"agent_session"`
	CurrentCommand string `json:"current_command,omitempty"`
	Revision       uint64 `json:"revision"`
}

type Tab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
	AgentStatus string `json:"agent_status"`
}

type Workspace struct {
	WorkspaceID string             `json:"workspace_id"`
	Number      int                `json:"number"`
	Label       string             `json:"label"`
	Focused     bool               `json:"focused"`
	PaneCount   int                `json:"pane_count"`
	TabCount    int                `json:"tab_count"`
	ActiveTabID string             `json:"active_tab_id"`
	AgentStatus string             `json:"agent_status"`
	Worktree    *WorkspaceWorktree `json:"worktree,omitempty"`
}

type WorkspaceWorktree struct {
	RepoRoot         string `json:"repo_root"`
	CheckoutPath     string `json:"checkout_path"`
	IsLinkedWorktree bool   `json:"is_linked_worktree"`
}

// UnmarshalJSON accepts both herdr agent_session shapes: a bare session ID or
// an object whose value member carries that ID.
func (p *Pane) UnmarshalJSON(data []byte) error {
	type alias Pane
	aux := struct {
		*alias
		AgentSession json.RawMessage `json:"agent_session"`
	}{alias: (*alias)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.AgentSession = ""
	trimmed := bytes.TrimSpace(aux.AgentSession)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		return json.Unmarshal(trimmed, &p.AgentSession)
	}
	var object struct {
		Agent string `json:"agent"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return fmt.Errorf("pane %s: agent_session: %w", p.PaneID, err)
	}
	p.AgentSession = object.Value
	if p.Agent == "" {
		// The live Herdr shape can carry the authoritative tool only inside
		// agent_session. Preserve that evidence so the shared fleet join can
		// use its existing unambiguous tool/session fallback.
		p.Agent = object.Agent
	}
	return nil
}

type Snapshot struct {
	Protocol   int         `json:"protocol"`
	Version    string      `json:"version"`
	Workspaces []Workspace `json:"workspaces"`
	Tabs       []Tab       `json:"tabs"`
	Agents     []Agent     `json:"agents"`
	Panes      []Pane      `json:"panes"`
}

// LiveSnapshot reads one session.snapshot directly from the herdr Unix
// socket. The CLI is used only to discover that socket and its protocol; no
// CLI-list fallback is allowed because it could hide a placement outage.
func LiveSnapshot() (Snapshot, error) {
	socket, err := liveSocket()
	if err != nil {
		return Snapshot{}, err
	}
	return snapshotFromSocket(socket)
}

// PaneProcessName returns Herdr's foreground process-group leader name for a
// pane. It preserves Herdr's reported name except for surrounding whitespace.
func PaneProcessName(paneID string) (string, error) {
	if strings.TrimSpace(paneID) == "" {
		return "", fmt.Errorf("pane process info requires a pane ID")
	}
	socket, err := liveSocket()
	if err != nil {
		return "", err
	}
	return paneProcessNameFromSocket(socket, paneID)
}

// PaneProcessNames resolves the Herdr socket once for a roster refresh and
// reads each requested pane independently. Individual lookup failures are
// omitted so callers can retain their existing display labels.
func PaneProcessNames(paneIDs []string) (map[string]string, error) {
	result := make(map[string]string, len(paneIDs))
	if len(paneIDs) == 0 {
		return result, nil
	}
	socket, err := liveSocket()
	if err != nil {
		return result, err
	}
	for _, paneID := range paneIDs {
		name, processErr := paneProcessNameFromSocket(socket, paneID)
		if processErr == nil && name != "" {
			result[paneID] = name
		}
	}
	return result, nil
}

func liveSocket() (string, error) {
	if socket := os.Getenv("HERDER_HERDR_SOCKET"); socket != "" {
		return socket, nil
	}
	out, err := exec.Command("herdr", "status", "server").Output()
	if err != nil {
		return "", fmt.Errorf("herdr status server failed: %w", err)
	}
	status, err := parseServerStatus(out)
	if err != nil {
		return "", err
	}
	if !supportsServerProtocol(status) {
		return "", fmt.Errorf("herdr protocol incompatible: client supports %d, server reported %d (compatible=%t)", supportedProtocol, status.protocol, status.compatible)
	}
	if status.socket == "" {
		return "", fmt.Errorf("herdr server did not report a socket")
	}
	return status.socket, nil
}

type serverStatus struct {
	socket     string
	protocol   int
	compatible bool
}

// A newer server may extend the protocol without breaking older clients. Herdr
// reports that relationship explicitly; older servers cannot satisfy fields a
// client built for this protocol may require.
func supportsServerProtocol(status serverStatus) bool {
	return status.compatible && status.protocol >= supportedProtocol
}

func parseServerStatus(out []byte) (serverStatus, error) {
	var envelope struct {
		Result struct {
			Socket     string      `json:"socket"`
			Protocol   json.Number `json:"protocol"`
			Compatible any         `json:"compatible"`
		} `json:"result"`
		Socket     string      `json:"socket"`
		Protocol   json.Number `json:"protocol"`
		Compatible any         `json:"compatible"`
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err == nil {
		status := serverStatus{
			socket:     firstNonEmpty(envelope.Result.Socket, envelope.Socket),
			protocol:   firstNonZero(numberInt(envelope.Result.Protocol), numberInt(envelope.Protocol)),
			compatible: compatibility(firstNonNil(envelope.Result.Compatible, envelope.Compatible)),
		}
		if status.socket == "" {
			return serverStatus{}, fmt.Errorf("herdr status server did not report a socket")
		}
		return status, nil
	}

	var status serverStatus
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "socket":
			status.socket = strings.TrimSpace(value)
		case "protocol":
			status.protocol, _ = strconv.Atoi(strings.TrimSpace(value))
		case "compatible":
			status.compatible = compatibility(strings.TrimSpace(value))
		}
	}
	if status.socket == "" {
		return serverStatus{}, fmt.Errorf("could not decode herdr server status")
	}
	return status, nil
}

func snapshotFromSocket(socket string) (Snapshot, error) {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return Snapshot{}, fmt.Errorf("connect herdr socket %s: %w", socket, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := []byte(`{"id":"herder-list-snapshot","method":"session.snapshot","params":{}}` + "\n")
	if _, err := conn.Write(request); err != nil {
		return Snapshot{}, fmt.Errorf("request herdr session.snapshot: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var response struct {
			ID     any             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil || fmt.Sprint(response.ID) != "herder-list-snapshot" {
			continue
		}
		if response.Error != nil {
			return Snapshot{}, fmt.Errorf("herdr session.snapshot: %s: %s", response.Error.Code, response.Error.Message)
		}
		snapshot, err := ParseSessionSnapshotResult(response.Result)
		if err != nil {
			return Snapshot{}, fmt.Errorf("decode herdr session.snapshot: %w", err)
		}
		return snapshot, nil
	}
	if err := scanner.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("read herdr session.snapshot: %w", err)
	}
	return Snapshot{}, fmt.Errorf("herdr socket closed before session.snapshot response")
}

func paneProcessNameFromSocket(socket, paneID string) (string, error) {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("connect herdr socket %s: %w", socket, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := map[string]any{
		"id":     "herder-pane-process-info",
		"method": "pane.process_info",
		"params": map[string]string{"pane_id": paneID},
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return "", fmt.Errorf("request herdr pane.process_info: %w", err)
	}
	var response struct {
		ID     any             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return "", fmt.Errorf("read herdr pane.process_info: %w", err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("herdr pane.process_info: %s: %s", response.Error.Code, response.Error.Message)
	}
	return ParsePaneProcessName(response.Result)
}

// ParsePaneProcessName decodes the real pane.process_info result shape.
func ParsePaneProcessName(result []byte) (string, error) {
	var payload struct {
		ProcessInfo struct {
			ForegroundProcesses []struct {
				Name string `json:"name"`
			} `json:"foreground_processes"`
		} `json:"process_info"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", fmt.Errorf("decode herdr pane.process_info: %w", err)
	}
	if len(payload.ProcessInfo.ForegroundProcesses) == 0 {
		return "", nil
	}
	return strings.TrimSpace(payload.ProcessInfo.ForegroundProcesses[0].Name), nil
}

// ParseSessionSnapshotResult accepts the live nested snapshot result and the
// direct snapshot form retained for compatibility with earlier herdr builds.
func ParseSessionSnapshotResult(result []byte) (Snapshot, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(result, &object); err != nil {
		return Snapshot{}, err
	}
	if raw, ok := object["snapshot"]; ok {
		var snapshot Snapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return Snapshot{}, err
		}
		if emptySnapshot(snapshot) {
			return Snapshot{}, fmt.Errorf("herdr session snapshot payload is empty")
		}
		return snapshot, nil
	}
	var snapshot Snapshot
	if err := json.Unmarshal(result, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if emptySnapshot(snapshot) {
		return Snapshot{}, fmt.Errorf("herdr session snapshot payload has no snapshot")
	}
	return snapshot, nil
}

func emptySnapshot(snapshot Snapshot) bool {
	return snapshot.Protocol == 0 && len(snapshot.Panes) == 0 && len(snapshot.Agents) == 0
}

func numberInt(value json.Number) int {
	n, _ := value.Int64()
	return int(n)
}

func compatibility(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(typed) {
		case "yes", "true", "1":
			return true
		}
	}
	return false
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
