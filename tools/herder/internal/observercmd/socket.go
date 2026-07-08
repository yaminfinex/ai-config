package observercmd

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
	"sync"
	"time"

	"ai-config/tools/herder/internal/herdrcli"
)

type socketStatus struct {
	socket     string
	protocol   int
	compatible bool
	detail     string
}

type socketResponse struct {
	result json.RawMessage
	err    error
}

type herdrSocketClient struct {
	conn    net.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int
	pending map[string]chan socketResponse
	events  chan json.RawMessage
	closed  chan struct{}
}

func connectHerdrSocket() (*herdrSocketClient, socketStatus, error) {
	st, err := discoverHerdrSocket()
	if err != nil {
		return nil, st, err
	}
	if !st.compatible {
		return nil, st, fmt.Errorf("herdr protocol incompatible: %s", st.detail)
	}
	conn, err := net.DialTimeout("unix", st.socket, 2*time.Second)
	if err != nil {
		return nil, st, err
	}
	c := &herdrSocketClient{
		conn:    conn,
		pending: map[string]chan socketResponse{},
		events:  make(chan json.RawMessage, 32),
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c, st, nil
}

func discoverHerdrSocket() (socketStatus, error) {
	if sock := os.Getenv("HERDER_OBSERVER_HERDR_SOCKET"); sock != "" {
		return socketStatus{socket: sock, compatible: true, detail: "env socket"}, nil
	}
	out, err := exec.Command("herdr", "status", "server").Output()
	if err != nil {
		return socketStatus{detail: "herdr status server unavailable"}, err
	}
	st := parseStatusServer(out)
	if st.socket == "" {
		return st, fmt.Errorf("herdr status server did not report socket")
	}
	if st.detail == "" {
		st.detail = "protocol compatible"
	}
	return st, nil
}

func parseStatusServer(out []byte) socketStatus {
	var env struct {
		Result struct {
			Socket     string      `json:"socket"`
			Protocol   json.Number `json:"protocol"`
			Compatible any         `json:"compatible"`
		} `json:"result"`
		Socket     string      `json:"socket"`
		Protocol   json.Number `json:"protocol"`
		Compatible any         `json:"compatible"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	if err := dec.Decode(&env); err == nil {
		socket := firstNonEmpty(env.Result.Socket, env.Socket)
		proto := numberInt(env.Result.Protocol)
		if proto == 0 {
			proto = numberInt(env.Protocol)
		}
		compatible := compatBool(env.Result.Compatible)
		if env.Result.Compatible == nil {
			compatible = compatBool(env.Compatible)
		}
		return socketStatus{socket: socket, protocol: proto, compatible: compatible, detail: fmt.Sprintf("protocol=%d", proto)}
	}
	st := socketStatus{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "socket":
			st.socket = val
		case "protocol":
			st.protocol, _ = strconv.Atoi(val)
		case "compatible":
			st.compatible = val == "yes" || val == "true" || val == "1"
		}
	}
	st.detail = fmt.Sprintf("protocol=%d compatible=%t", st.protocol, st.compatible)
	return st
}

func numberInt(n json.Number) int {
	if n == "" {
		return 0
	}
	i, _ := n.Int64()
	return int(i)
}

func compatBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "yes" || x == "true" || x == "1"
	default:
		return false
	}
}

func (c *herdrSocketClient) Close() {
	_ = c.conn.Close()
	<-c.closed
}

func (c *herdrSocketClient) readLoop() {
	defer close(c.closed)
	scanner := bufio.NewScanner(c.conn)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg struct {
			ID     any             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		id := idString(msg.ID)
		if id != "" {
			c.mu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if ch != nil {
				resp := socketResponse{result: msg.Result}
				if msg.Error != nil {
					resp.err = fmt.Errorf("%s: %s", msg.Error.Code, msg.Error.Message)
				}
				ch <- resp
			}
			continue
		}
		select {
		case c.events <- append(json.RawMessage(nil), line...):
		default:
		}
	}
	c.mu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- socketResponse{err: fmt.Errorf("herdr socket closed")}
	}
	c.mu.Unlock()
}

func idString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

func (c *herdrSocketClient) call(method string, params any, out any) error {
	c.mu.Lock()
	c.nextID++
	id := strconv.Itoa(c.nextID)
	ch := make(chan socketResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	req := map[string]any{"id": id, "method": method, "params": params}
	if req["params"] == nil {
		req["params"] = map[string]any{}
	}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	_, err = c.conn.Write(append(b, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case resp := <-ch:
		if resp.err != nil {
			return resp.err
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(resp.result, out)
	case <-time.After(3 * time.Second):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("timeout waiting for herdr %s", method)
	}
}

func (c *herdrSocketClient) snapshot() (herdrcli.Snapshot, error) {
	var snap herdrcli.Snapshot
	err := c.call("session.snapshot", map[string]any{}, &snap)
	return snap, err
}

func (c *herdrSocketClient) processInfo(paneID string) (herdrcli.ProcessInfo, error) {
	var raw json.RawMessage
	if err := c.call("pane.process_info", map[string]any{"pane_id": paneID}, &raw); err != nil {
		return herdrcli.ProcessInfo{}, err
	}
	var wrapped struct {
		ProcessInfo *herdrcli.ProcessInfo `json:"process_info"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.ProcessInfo != nil {
		return *wrapped.ProcessInfo, nil
	}
	var direct herdrcli.ProcessInfo
	if err := json.Unmarshal(raw, &direct); err != nil {
		return herdrcli.ProcessInfo{}, err
	}
	return direct, nil
}

func (c *herdrSocketClient) subscribeObserverEvents() error {
	subs := []map[string]string{
		{"type": "pane.created"},
		{"type": "pane.closed"},
		{"type": "pane.exited"},
		{"type": "pane.agent_detected"},
	}
	return c.call("events.subscribe", map[string]any{"subscriptions": subs}, nil)
}

func (c *herdrSocketClient) nextEvent(timeout time.Duration) bool {
	select {
	case <-c.events:
		return true
	case <-c.closed:
		return false
	case <-time.After(timeout):
		return false
	}
}
