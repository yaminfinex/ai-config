package grokbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockBridge struct {
	b      *Binder
	cancel context.CancelFunc
}

func startMockBridge(t *testing.T, state string, session string) *mockBridge {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: bin, SessionID: session})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(b.socket); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", b.socket)
	if err != nil {
		t.Fatal(err)
	}
	b.listener = ln
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.acceptLoop(ctx) }()
	return &mockBridge{b: b, cancel: cancel}
}
func (m *mockBridge) close() {
	m.cancel()
	if m.b.listener != nil {
		m.b.listener.Close()
	}
	m.b.Close()
}
func (m *mockBridge) client(t *testing.T) *Client {
	t.Helper()
	c, err := dialClient(m.b.socket, m.b.cfg.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (m *mockBridge) queue(t *testing.T, id int64, text string) Receipt {
	t.Helper()
	r, added, err := m.b.journal.Queue(rawEvent(t, id, text))
	if err != nil || !added {
		t.Fatalf("queue added=%v err=%v", added, err)
	}
	if err := m.b.wake(r, "wake"); err != nil {
		t.Fatal(err)
	}
	return r
}

type tapProbe struct {
	conn  net.Conn
	lines chan string
	done  chan struct{}
}

func connectTap(t *testing.T, socket, session string) *tapProbe {
	t.Helper()
	c, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(c).Encode(Request{Op: "tap", SessionID: session}); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(c)
	var hello Response
	line, err := br.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(line, &hello); err != nil {
		t.Fatal(err)
	}
	if !hello.OK {
		t.Fatalf("tap handshake: %s", hello.Error)
	}
	p := &tapProbe{conn: c, lines: make(chan string, 16), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		for {
			line, err := br.ReadString('\n')
			if line != "" {
				p.lines <- strings.TrimSpace(line)
			}
			if err != nil {
				return
			}
		}
	}()
	return p
}
func (p *tapProbe) close() { p.conn.Close(); <-p.done }
func (p *tapProbe) next(timeout time.Duration) (string, bool) {
	select {
	case line := <-p.lines:
		return line, true
	case <-time.After(timeout):
		return "", false
	}
}

func runMockMCP(t *testing.T, socket string, requests ...string) []rpcResponse {
	t.Helper()
	var out bytes.Buffer
	input := strings.NewReader(strings.Join(requests, "\n") + "\n")
	if err := ServeMCP(socket, input, &out); err != nil {
		t.Fatal(err)
	}
	var responses []rpcResponse
	s := bufio.NewScanner(&out)
	for s.Scan() {
		var r rpcResponse
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, r)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return responses
}

func TestT1InitialDeliveryThroughPendingFetchAck(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	m.queue(t, 1, "initial task")
	t.Setenv("HERDER_GROK_SESSION_ID", "owner")
	responses := runMockMCP(t, m.b.socket,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_pending","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"fetch_message","arguments":{"id":1}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ack_message","arguments":{"id":1}}}`,
	)
	if len(responses) != 4 {
		t.Fatalf("responses=%d", len(responses))
	}
	for _, r := range responses {
		if r.Error != nil {
			t.Fatalf("MCP response error: %+v", r.Error)
		}
	}
	if got := m.b.journal.receipts[1].Status(); got != "delivered" {
		t.Fatalf("status=%s", got)
	}
}

func TestT2IdleDeliveryThroughTapFetchAck(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	tap := connectTap(t, m.b.socket, "owner")
	defer tap.close()
	m.queue(t, 2, "idle")
	if line, ok := tap.next(time.Second); !ok || !strings.HasPrefix(line, "HCOM id=2 ") {
		t.Fatalf("wake=%q ok=%v", line, ok)
	}
	c := m.client(t)
	if _, err := c.Call(Request{Op: "fetch", ID: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(Request{Op: "ack", ID: 2}); err != nil {
		t.Fatal(err)
	}
	if m.b.journal.receipts[2].Status() != "delivered" {
		t.Fatal("not delivered")
	}
}

func TestT3BusyTurnDefersIdleAwareNudge(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owner")
	defer m.close()
	tap := connectTap(t, m.b.socket, "owner")
	defer tap.close()
	events := filepath.Join(state, "events.jsonl")
	if err := os.WriteFile(events, []byte("{\"event\":\"phase_changed\",\"phase\":\"tool_execution\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.b.cfg.SessionEvents = events
	m.b.cfg.NudgeAfter = 60 * time.Millisecond
	m.b.cfg.MaxNudges = 2
	m.queue(t, 3, "busy")
	if _, ok := tap.next(time.Second); !ok {
		t.Fatal("initial wake missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.b.nudgeLoop(ctx) }()
	if line, ok := tap.next(180 * time.Millisecond); ok {
		t.Fatalf("busy turn received nudge %q", line)
	}
	if err := os.WriteFile(events, []byte("{\"event\":\"phase_changed\",\"phase\":\"idle\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if line, ok := tap.next(time.Second); !ok || !strings.HasPrefix(line, "HCOM id=3 ") {
		t.Fatalf("idle nudge line=%q ok=%v", line, ok)
	}
}

func TestT4DuplicateWakeThroughTapIsIdempotent(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	tap := connectTap(t, m.b.socket, "owner")
	defer tap.close()
	r := m.queue(t, 4, "duplicate")
	first, ok := tap.next(time.Second)
	if !ok {
		t.Fatal("first wake missing")
	}
	if err := m.b.wake(r, "nudge"); err != nil {
		t.Fatal(err)
	}
	second, ok := tap.next(time.Second)
	if !ok || second != first {
		t.Fatalf("duplicate wake first=%q second=%q", first, second)
	}
	c := m.client(t)
	if _, err := c.Call(Request{Op: "fetch", ID: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(Request{Op: "fetch", ID: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(Request{Op: "ack", ID: 4}); err != nil {
		t.Fatal(err)
	}
	if !m.b.journal.receipts[4].Acked {
		t.Fatal("message not delivered")
	}
}

func TestT5DuplicateAckThroughMCPIsIdempotent(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	m.queue(t, 5, "duplicate ack")
	c := m.client(t)
	if _, err := c.Call(Request{Op: "fetch", ID: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(Request{Op: "ack", ID: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(Request{Op: "ack", ID: 5}); err != nil {
		t.Fatal(err)
	}
	if !m.b.journal.receipts[5].Acked {
		t.Fatal("ack regressed")
	}
}

func TestT6OutOfOrderMessagesDeliverIndependentlyThroughMCP(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	m.queue(t, 3, "earlier")
	m.queue(t, 6, "later")
	c := m.client(t)
	for _, id := range []int64{6, 3} {
		if _, err := c.Call(Request{Op: "fetch", ID: id}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Call(Request{Op: "ack", ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if !m.b.journal.receipts[3].Acked || !m.b.journal.receipts[6].Acked {
		t.Fatal("independent delivery failed")
	}
}

func TestT7AckBeforeFetchRejectedThroughMCP(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	m.queue(t, 7, "order")
	c := m.client(t)
	if _, err := c.Call(Request{Op: "ack", ID: 7}); err == nil || !strings.Contains(err.Error(), "fetch before ack") {
		t.Fatalf("err=%v", err)
	}
	if m.b.journal.receipts[7].Acked {
		t.Fatal("rejected ack changed state")
	}
}

func TestT8ForeignMessageIDRejectedThroughMCP(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	c := m.client(t)
	if _, err := c.Call(Request{Op: "fetch", ID: 808}); err == nil || !strings.Contains(err.Error(), "not queued") {
		t.Fatalf("err=%v", err)
	}
}

func TestT9QueuedBeforeWakeRecoversThroughPendingMCP(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owner")
	if _, _, err := m.b.journal.Queue(rawEvent(t, 9, "before wake")); err != nil {
		t.Fatal(err)
	}
	m.close()
	m = startMockBridge(t, state, "owner")
	defer m.close()
	c := m.client(t)
	p, err := c.Call(Request{Op: "pending"})
	if err != nil || len(p.Pending) != 1 {
		t.Fatalf("pending=%+v err=%v", p, err)
	}
	if _, err = c.Call(Request{Op: "fetch", ID: 9}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Call(Request{Op: "ack", ID: 9}); err != nil {
		t.Fatal(err)
	}
}

func TestT10RestartEmitsSingleRecoveryWithoutPerIDRewake(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owner")
	tap := connectTap(t, m.b.socket, "owner")
	m.queue(t, 10, "one")
	m.queue(t, 11, "two")
	if _, ok := tap.next(time.Second); !ok {
		t.Fatal("first wake missing")
	}
	if _, ok := tap.next(time.Second); !ok {
		t.Fatal("second wake missing")
	}
	tap.close()
	m.close()
	m = startMockBridge(t, state, "owner")
	defer m.close()
	tap = connectTap(t, m.b.socket, "owner")
	defer tap.close()
	line, ok := tap.next(time.Second)
	if !ok || line != "HCOM_RECOVER pending=2" {
		t.Fatalf("recovery=%q ok=%v", line, ok)
	}
	if extra, ok := tap.next(180 * time.Millisecond); ok {
		t.Fatalf("unexpected per-id re-wake %q", extra)
	}
}

func TestT11TapDeathQueuesUntilRecoveryReconnect(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	tap := connectTap(t, m.b.socket, "owner")
	m.queue(t, 12, "before death")
	if _, ok := tap.next(time.Second); !ok {
		t.Fatal("wake missing")
	}
	tap.close()
	m.queue(t, 13, "while down")
	c := m.client(t)
	pending, err := c.Call(Request{Op: "pending"})
	if err != nil || len(pending.Pending) != 2 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	tap = connectTap(t, m.b.socket, "owner")
	defer tap.close()
	line, ok := tap.next(time.Second)
	if !ok || line != "HCOM_RECOVER pending=2" {
		t.Fatalf("recovery=%q ok=%v", line, ok)
	}
}

func TestT12FetchedNotAckedPersistsAcrossBridgeRestart(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owner")
	m.queue(t, 12, "retry")
	c := m.client(t)
	if _, err := c.Call(Request{Op: "fetch", ID: 12}); err != nil {
		t.Fatal(err)
	}
	m.close()
	m = startMockBridge(t, state, "owner")
	defer m.close()
	c = m.client(t)
	p, err := c.Call(Request{Op: "pending"})
	if err != nil || len(p.Pending) != 1 || p.Pending[0].Status != "fetched" {
		t.Fatalf("pending=%+v err=%v", p, err)
	}
	if _, err = c.Call(Request{Op: "ack", ID: 12}); err != nil {
		t.Fatal(err)
	}
}

func TestT13RecoveryRelistAfterMonitorReset(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	m.queue(t, 13, "compact")
	c := m.client(t)
	p, err := c.Call(Request{Op: "pending"})
	if err != nil || len(p.Pending) != 1 {
		t.Fatalf("pending=%+v err=%v", p, err)
	}
	if !m.b.journal.receipts[13].Surfaced {
		t.Fatal("re-list did not surface")
	}
}

func TestT14SameSeatRestartRelistsPendingWithoutRegression(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owner")
	m.queue(t, 14, "resume")
	m.close()
	m = startMockBridge(t, state, "owner")
	defer m.close()
	c := m.client(t)
	p, err := c.Call(Request{Op: "pending"})
	if err != nil || len(p.Pending) != 1 || p.Pending[0].ID != 14 {
		t.Fatalf("pending=%+v err=%v", p, err)
	}
}

func TestT15FreshSeatCannotFetchParentPendingMessage(t *testing.T) {
	parent := startMockBridge(t, t.TempDir(), "owner")
	defer parent.close()
	parent.queue(t, 15, "parent")
	child := startMockBridge(t, t.TempDir(), "owner")
	defer child.close()
	c := child.client(t)
	if _, err := c.Call(Request{Op: "fetch", ID: 15}); err == nil {
		t.Fatal("fresh seat fetched parent message")
	}
	if parent.b.journal.receipts[15].Status() == "delivered" {
		t.Fatal("parent receipt changed")
	}
}

func TestT16SubagentBoundaryRejectsForeignAndUnownedSessionEvidence(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	m.queue(t, 16, "protected")
	before := m.b.journal.receipts[16].Status()
	if _, err := dialClient(m.b.socket, "foreign"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("foreign handshake err=%v", err)
	}
	if got := m.b.journal.receipts[16].Status(); got != before {
		t.Fatalf("foreign request changed state %s -> %s", before, got)
	}
	if _, err := dialClient(m.b.socket, ""); err == nil || !strings.Contains(err.Error(), "omitted session evidence") {
		t.Fatalf("omitted evidence err=%v", err)
	}
	m.b.cfg.SessionID = ""
	if _, err := dialClient(m.b.socket, "foreign"); err == nil || !strings.Contains(err.Error(), "no owning session") {
		t.Fatalf("unowned bridge handshake err=%v", err)
	}
}

func TestLifecycleStatusAndRetirementUseGenerationFencedSocket(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	m.queue(t, 31, "delivered")
	m.queue(t, 32, "pending")
	if _, err := m.b.journal.Fetch(31, m.b.generation); err != nil {
		t.Fatal(err)
	}
	if err := m.b.journal.Ack(31, m.b.generation); err != nil {
		t.Fatal(err)
	}

	c := m.client(t)
	status, err := c.Call(Request{Op: "status"})
	if err != nil || status.Status == nil || status.Status.PID != os.Getpid() || status.Status.Bus != "bound" || status.Status.Wake != "degraded" || status.Status.Pending != 1 {
		t.Fatalf("degraded status=%+v err=%v", status.Status, err)
	}
	tap := connectTap(t, m.b.socket, "owner")
	defer tap.close()
	deadline := time.Now().Add(time.Second)
	for {
		status, err = c.Call(Request{Op: "status"})
		if err == nil && status.Status != nil && status.Status.Wake == "armed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("armed status=%+v err=%v", status.Status, err)
		}
		time.Sleep(time.Millisecond)
	}

	stale, err := roundTrip(m.b.socket, Request{Op: "retire", Generation: m.b.generation - 1, SessionID: "owner"})
	if err == nil || !strings.HasPrefix(stale.Error, "stale bridge generation ") || m.b.journal.receipts[32].Retired {
		t.Fatalf("stale retire response=%+v err=%v retired=%v", stale, err, m.b.journal.receipts[32].Retired)
	}
	retired, err := c.Call(Request{Op: "retire"})
	if err != nil || retired.Retired != 1 {
		t.Fatalf("retire response=%+v err=%v", retired, err)
	}
	select {
	case <-m.b.retired:
	case <-time.After(time.Second):
		t.Fatal("binder did not receive orderly retirement request")
	}
	if m.b.journal.receipts[31].Status() != "delivered" || m.b.journal.receipts[32].Status() != "undeliverable" {
		t.Fatalf("statuses=%s,%s", m.b.journal.receipts[31].Status(), m.b.journal.receipts[32].Status())
	}
}

func TestRetirementResponseStopsServingBinderOrderly(t *testing.T) {
	state := t.TempDir()
	hcom := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(hcom, []byte("#!/bin/sh\nif [ \"$1\" = start ]; then printf '%s\\n' '[hcom:seat-bus]'; exit 0; fi\ncase \" $* \" in *' --wait '*) exec sleep 60;; esac\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: hcom, SessionID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	serveErr := make(chan error, 1)
	go func() { serveErr <- b.Serve(context.Background()) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if st, statErr := os.Stat(b.socket); statErr == nil && st.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("binder socket did not become ready")
		}
		time.Sleep(time.Millisecond)
	}
	c, err := dialClient(b.socket, "owner")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Call(Request{Op: "retire"})
	if err != nil || !resp.OK {
		t.Fatalf("retirement response=%+v err=%v", resp, err)
	}
	select {
	case err = <-serveErr:
		if !errors.Is(err, errSeatRetired) {
			t.Fatalf("serve error=%v, want retirement sentinel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("binder continued serving after retirement response")
	}
}

func TestClientStraddlesBinderRestartReconnectsOnceAndDelivers(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owner")
	client := m.client(t)
	m.close()
	m = startMockBridge(t, state, "owner")
	defer m.close()
	m.queue(t, 21, "after restart")
	pending, err := client.Call(Request{Op: "pending"})
	if err != nil || len(pending.Pending) != 1 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if _, err = client.Call(Request{Op: "fetch", ID: 21}); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Call(Request{Op: "ack", ID: 21}); err != nil {
		t.Fatal(err)
	}
	if got := m.b.journal.receipts[21].Status(); got != "delivered" {
		t.Fatalf("status=%s", got)
	}
}

func TestPersistentMCPServerStraddlesBinderRestart(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owner")
	t.Setenv("HERDER_GROK_SESSION_ID", "owner")
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	done := make(chan error, 1)
	go func() { err := ServeMCP(m.b.socket, inReader, outWriter); outWriter.Close(); done <- err }()
	responses := bufio.NewReader(outReader)
	call := func(payload string) {
		t.Helper()
		if _, err := io.WriteString(inWriter, payload+"\n"); err != nil {
			t.Fatal(err)
		}
		line, err := responses.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response map[string]any
		if err = json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatal(err)
		}
		if result, ok := response["result"].(map[string]any); ok {
			if failed, _ := result["isError"].(bool); failed {
				t.Fatalf("MCP error response: %s", line)
			}
		}
	}
	call(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_pending","arguments":{}}}`)
	m.close()
	m = startMockBridge(t, state, "owner")
	defer m.close()
	m.queue(t, 22, "persistent MCP")
	call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_pending","arguments":{}}}`)
	call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"fetch_message","arguments":{"id":22}}}`)
	call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ack_message","arguments":{"id":22}}}`)
	if got := m.b.journal.receipts[22].Status(); got != "delivered" {
		t.Fatalf("status=%s", got)
	}
	inWriter.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("MCP server did not stop")
	}
	outReader.Close()
}

func TestT17IdleBinderAndTapEmitZeroModelFacingBytes(t *testing.T) {
	state := t.TempDir()
	bin := filepath.Join(t.TempDir(), "hcom")
	script := "#!/bin/sh\nif [ \"$1\" = events ]; then\n  case \" $* \" in *\" --wait \"*) sleep 10; exit 1;; *) exit 0;; esac\nfi\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: bin, BusName: "bound", SessionID: "owner", Wait: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Serve(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(b.socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge socket did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	tap := connectTap(t, b.socket, "owner")
	defer tap.close()
	if line, ok := tap.next(300 * time.Millisecond); ok {
		t.Fatalf("idle tap emitted %q", line)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop")
	}
	b.Close()
}

func TestT18ReportingClaimsDeliveredOnlyAfterMCPAck(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	defer m.close()
	m.queue(t, 18, "report")
	if m.b.journal.receipts[18].Status() == "delivered" {
		t.Fatal("queue claimed delivery")
	}
	c := m.client(t)
	if _, err := c.Call(Request{Op: "fetch", ID: 18}); err != nil {
		t.Fatal(err)
	}
	if m.b.journal.receipts[18].Status() == "delivered" {
		t.Fatal("fetch claimed delivery")
	}
	if _, err := c.Call(Request{Op: "ack", ID: 18}); err != nil {
		t.Fatal(err)
	}
	if m.b.journal.receipts[18].Status() != "delivered" {
		t.Fatal("ack did not claim delivery")
	}
}

func TestSurfaceFailureIsDiagnosedAndTapDroppedForRecovery(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owner")
	tap := connectTap(t, m.b.socket, "owner")
	r, _, err := m.b.journal.Queue(rawEvent(t, 19, "surface failure"))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.b.journal.f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = m.b.wake(r, "wake"); err == nil || !strings.Contains(err.Error(), "tap dropped") {
		t.Fatalf("wake err=%v", err)
	}
	if _, ok := tap.next(time.Second); !ok {
		t.Fatal("wake was not handed to tap")
	}
	select {
	case <-tap.done:
	case <-time.After(time.Second):
		t.Fatal("tap not dropped after surface failure")
	}
	data, err := os.ReadFile(filepath.Join(state, "grok", "seat", "bridge.log"))
	if err != nil || !strings.Contains(string(data), "reconnect recovery can re-list") {
		t.Fatalf("diagnostic=%q err=%v", data, err)
	}
	m.close()
	m = startMockBridge(t, state, "owner")
	defer m.close()
	recovery := connectTap(t, m.b.socket, "owner")
	defer recovery.close()
	if line, ok := recovery.next(time.Second); !ok || line != "HCOM_RECOVER pending=1" {
		t.Fatalf("recovery=%q ok=%v", line, ok)
	}
}

func TestTapClientPreservesImmediateRecoveryLine(t *testing.T) {
	m := startMockBridge(t, t.TempDir(), "owner")
	m.queue(t, 20, "pending")
	t.Setenv("HERDER_GROK_SESSION_ID", "owner")
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { err := Tap(m.b.socket, writer); writer.Close(); done <- err }()
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "HCOM_RECOVER pending=1" {
		t.Fatalf("line=%q", line)
	}
	m.close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tap client did not stop")
	}
	reader.Close()
}
