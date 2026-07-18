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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

type mockBridge struct {
	b      *Binder
	cancel context.CancelFunc
}

const servingHcomScript = "#!/bin/sh\nif [ \"$1\" = start ]; then printf '%s\\n' '[hcom:seat-bus]'; exit 0; fi\nif [ \"$1\" = list ]; then printf '%s\\n' '{\"name\":\"seat-bus\"}'; exit 0; fi\ncase \" $* \" in *' --wait '*) exec sleep 60;; esac\nexit 0\n"

const repairingHcomScript = "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HCOM_DIR/calls\"\ncase \"$1\" in\n  list) [ -f \"$HCOM_DIR/joined\" ] || exit 1; printf '%s\\n' '{\"name\":\"seat-bus\"}' ;;\n  start) : > \"$HCOM_DIR/joined\"; printf '%s\\n' '[hcom:seat-bus]' ;;\n  send) printf '%s\\n' sent ;;\nesac\n"

func startMockBridge(t *testing.T, state string, session string) *mockBridge {
	return startMockBridgeForSeat(t, state, "seat", session)
}

func startMockBridgeForSeat(t *testing.T, state, seat string, session string) *mockBridge {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(bin, []byte(servingHcomScript), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: seat, StateDir: state, HcomBin: bin, BusName: "seat-bus", SessionID: session})
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

func TestStatusRepairsMissingBusRowBeforeReportingHealthy(t *testing.T) {
	state := t.TempDir()
	dir := t.TempDir()
	marker := filepath.Join(dir, "joined")
	bin := filepath.Join(dir, "hcom")
	if err := os.WriteFile(bin, []byte(repairingHcomScript), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: bin, HcomDir: dir, BusName: "seat-bus", SessionID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	resp := b.execute(Request{Op: "status", Generation: b.generation, SessionID: "owner"})
	if !resp.OK || resp.Status == nil || resp.Status.Bus != "seat-bus" {
		t.Fatalf("status after row repair=%+v", resp)
	}
	if _, err = os.Stat(marker); err != nil {
		t.Fatalf("status did not rebind missing row: %v", err)
	}
}

func TestStatusRefusesHealthyClaimWhenBusRowCannotBeRebound(t *testing.T) {
	state := t.TempDir()
	bin := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: bin, BusName: "seat-bus", SessionID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	resp := b.execute(Request{Op: "status", Generation: b.generation, SessionID: "owner"})
	if resp.OK || resp.Status != nil || !strings.Contains(resp.Error, "rebind") {
		t.Fatalf("status claimed health without a bus row: %+v", resp)
	}
}

func TestOutboundSendRepairsMissingBusRowBeforeSending(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hcom")
	if err := os.WriteFile(bin, []byte(repairingHcomScript), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: t.TempDir(), HcomBin: bin, HcomDir: dir, BusName: "seat-bus", SessionID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	resp := b.execute(Request{Op: "send", Generation: b.generation, SessionID: "owner", To: []string{"peer"}, Text: "report"})
	if !resp.OK || resp.Result != "sent" {
		t.Fatalf("send after row repair=%+v", resp)
	}
	data, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	calls := string(data)
	if !strings.Contains(calls, "start --as seat-bus") || !strings.Contains(calls, "send @peer --name seat-bus -- report") {
		t.Fatalf("row repair/send calls=%q", calls)
	}
}

func TestIdentityLoopRefreshesExactBusRow(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	bin := filepath.Join(dir, "hcom")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = list ]; then printf '%s\\n' \"$*\" >> \"$HCOM_DIR/calls\"; printf '%s\\n' '{\"name\":\"seat-bus\"}'; fi\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: t.TempDir(), HcomBin: bin, HcomDir: dir, BusName: "seat-bus", IdentityRefresh: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.identityLoop(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		if strings.Contains(string(data), "list seat-bus --name seat-bus --json") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("identity loop did not refresh exact row; calls=%q", data)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatalf("identity loop stop: %v", err)
	}
}

func TestIdentityLoopTreatsRetirementAsOrderlyStop(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: t.TempDir(), HcomBin: bin, BusName: "seat-bus", IdentityRefresh: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.retiring.Store(true)
	done := make(chan error, 1)
	go func() { done <- b.identityLoop(context.Background()) }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("identity loop retirement: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("identity loop did not stop after retirement")
	}
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
	r, added, err := m.b.queueReceipt(rawEvent(t, id, text))
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
	if err := m.b.wake(r, "wake"); err != nil {
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
	if err != nil || status.Status == nil || status.Status.PID != os.Getpid() || status.Status.Bus != "seat-bus" || status.Status.Wake != "degraded" || status.Status.Pending != 1 {
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
	if err := os.WriteFile(hcom, []byte(servingHcomScript), 0o700); err != nil {
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

func TestRetirementResponseArrivesBeforeBridgeSubprocessStops(t *testing.T) {
	state := t.TempDir()
	hcom := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(hcom, []byte(servingHcomScript), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestBridgeSubprocessHelper", "--", "--seat", "seat", "--state-dir", state, "--hcom-bin", hcom, "--session-id", "owner")
	cmd.Env = append(os.Environ(), "HERDER_BRIDGE_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	socket := SocketPath(state, "seat")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if st, err := os.Stat(socket); err == nil && st.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge subprocess socket did not become ready")
		}
		time.Sleep(time.Millisecond)
	}
	client, err := dialClient(socket, "owner")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Call(Request{Op: "retire"})
	if err != nil || !resp.OK {
		t.Fatalf("retirement response=%+v err=%v", resp, err)
	}
	err = cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 24 {
		t.Fatalf("bridge subprocess exit=%v, want code 24 after response", err)
	}
}

func TestBridgeSubprocessHelper(t *testing.T) {
	if os.Getenv("HERDER_BRIDGE_HELPER") != "1" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		os.Exit(125)
	}
	os.Exit(runBridge(os.Args[separator+1:], os.Stderr))
}

func TestBinderPublishesEventDrivenCapabilities(t *testing.T) {
	state := t.TempDir()
	seedGrokRegistryRow(t, state, "seat", "owner")
	m := startMockBridge(t, state, "owner")
	defer m.close()
	m.queue(t, 71, "pending")
	tap := connectTap(t, m.b.socket, "owner")
	waitForWakeCapability(t, state, "seat", "armed", 1, 0)
	tap.close()
	waitForWakeCapability(t, state, "seat", "degraded", 1, 0)
	client := m.client(t)
	resp, err := client.Call(Request{Op: "retire"})
	if err != nil || resp.Retired != 1 {
		t.Fatalf("retire response=%+v err=%v", resp, err)
	}
	waitForWakeCapability(t, state, "seat", "down", 0, 1)
	projection, err := v2.LoadFile(filepath.Join(state, "registry.jsonl"), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	foreign := registry.V2ByGUID(projection, "foreign-seat")
	if foreign == nil || foreign.Capabilities != nil || foreign.RecordedAt != "2026-07-13T00:00:01Z" {
		t.Fatalf("foreign row was changed by seat capability publication: %+v", foreign)
	}
}

func TestArmedBinderPublishesExactPendingCountWithoutTapFlap(t *testing.T) {
	state := t.TempDir()
	seedGrokRegistryRow(t, state, "seat", "owner")
	m := startMockBridge(t, state, "owner")
	defer m.close()
	tap := connectTap(t, m.b.socket, "owner")
	tapClosed := false
	defer func() {
		if !tapClosed {
			tap.close()
		}
	}()
	waitForWakeCapability(t, state, "seat", "armed", 0, 0)
	m.queue(t, 72, "first pending message")
	waitForWakeCapability(t, state, "seat", "armed", 1, 0)
	m.queue(t, 73, "second pending message")
	m.queue(t, 74, "third pending message")
	waitForWakeCapability(t, state, "seat", "armed", 3, 0)
	client := m.client(t)
	if _, err := client.Call(Request{Op: "fetch", ID: 72}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Call(Request{Op: "ack", ID: 72}); err != nil {
		t.Fatal(err)
	}
	waitForWakeCapability(t, state, "seat", "armed", 2, 0)
	for _, id := range []int64{73, 74} {
		if _, err := client.Call(Request{Op: "fetch", ID: id}); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Call(Request{Op: "ack", ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	waitForWakeCapability(t, state, "seat", "armed", 0, 0)
	tap.close()
	tapClosed = true
	waitForWakeCapability(t, state, "seat", "degraded", 0, 0)
}

func TestRetirementPublishFailureIsDiagnosticAndStillStops(t *testing.T) {
	state := shortState(t)
	seedGrokRegistryRow(t, state, "seat", "owner")
	retireGrokRegistryRow(t, state, "seat")
	hcom := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(hcom, []byte(servingHcomScript), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: hcom, SessionID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = b.Close()
		}
	})
	if _, added, queueErr := b.journal.Queue(rawEvent(t, 73, "retire despite registry")); queueErr != nil || !added {
		t.Fatalf("queue added=%v err=%v", added, queueErr)
	}
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
	client, err := dialClient(b.socket, "owner")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Call(Request{Op: "retire"})
	if err != nil || !resp.OK || resp.Retired != 1 {
		t.Fatalf("retirement response=%+v err=%v", resp, err)
	}
	select {
	case err = <-serveErr:
		if !errors.Is(err, errSeatRetired) {
			t.Fatalf("serve error=%v, want retirement sentinel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("binder became a zombie after failed capability publication")
	}
	diagnostic := filepath.Join(state, "grok", "seat", "bridge.log")
	data, err := os.ReadFile(diagnostic)
	if err != nil || !strings.Contains(string(data), "record down capability") {
		t.Fatalf("diagnostic=%q err=%v", data, err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	retired, err := RetireOffline(state, "seat")
	if err != nil || retired != 1 {
		t.Fatalf("offline convergence after orderly stop retired=%d err=%v", retired, err)
	}
}

func TestWakeCapabilityPublishFailureIsDiagnosticAndBinderSurvives(t *testing.T) {
	state := t.TempDir()
	seedGrokRegistryRow(t, state, "seat", "owner")
	retireGrokRegistryRow(t, state, "seat")
	m := startMockBridge(t, state, "owner")
	defer m.close()
	server, peer := net.Pipe()
	peer.Close()
	defer server.Close()
	m.b.mu.Lock()
	m.b.taps[server] = struct{}{}
	m.b.mu.Unlock()
	receipt, added, err := m.b.journal.Queue(rawEvent(t, 74, "wake despite registry"))
	if err != nil || !added {
		t.Fatalf("queue added=%v err=%v", added, err)
	}
	if err = m.b.wake(receipt, "wake"); err != nil {
		t.Fatalf("wake failed on derived registry publication: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(state, "grok", "seat", "bridge.log"))
	if err != nil || !strings.Contains(string(data), "record degraded capability") {
		t.Fatalf("diagnostic=%q err=%v", data, err)
	}
	if _, err = m.client(t).Call(Request{Op: "status"}); err != nil {
		t.Fatalf("binder did not survive capability publication failure: %v", err)
	}
}

func TestRetirementCountsReceiptQueuedByInFlightDrain(t *testing.T) {
	state := t.TempDir()
	seedGrokRegistryRow(t, state, "seat", "owner")
	m := startMockBridge(t, state, "owner")
	defer m.close()
	m.queue(t, 81, "before retire")
	m.b.drainMu.Lock()
	response := make(chan Response, 1)
	go func() {
		response <- m.b.execute(Request{Op: "retire", Generation: m.b.generation, SessionID: "owner"})
	}()
	deadline := time.Now().Add(time.Second)
	for !m.b.retiring.Load() {
		if time.Now().After(deadline) {
			t.Fatal("retirement did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	if _, added, err := m.b.journal.Queue(rawEvent(t, 82, "in-flight after retire")); err != nil || !added {
		t.Fatalf("in-flight queue added=%v err=%v", added, err)
	}
	m.b.drainMu.Unlock()
	resp := <-response
	if !resp.OK || resp.Retired != 2 {
		t.Fatalf("retirement response=%+v, want both ids counted", resp)
	}
	if got := m.b.journal.receipts[82].Status(); got != "undeliverable" {
		t.Fatalf("post-retire queued status=%s", got)
	}
	if err := m.b.publishCapabilities("armed"); err != nil {
		t.Fatalf("late live capability publication after retire: %v", err)
	}
	waitForWakeCapability(t, state, "seat", "down", 0, 2)
}

func TestManualSupervisorStopConvergesPendingJournalToRetired(t *testing.T) {
	state := t.TempDir()
	seat := "manual-seat"
	journal, err := OpenJournal(filepath.Join(SeatDir(state, seat), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, added, queueErr := journal.Queue(rawEvent(t, 91, "pending when wrapper dies")); queueErr != nil || !added {
		t.Fatalf("queue added=%v err=%v", added, queueErr)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "started")
	child := filepath.Join(t.TempDir(), "bridge-child")
	if err = os.WriteFile(child, []byte("#!/bin/sh\n: > \"$HERDER_TEST_SUPERVISOR_STARTED\"\nwhile :; do sleep 1; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_TEST_SUPERVISOR_STARTED", marker)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan int, 1)
	go func() {
		result <- superviseBridgeContext(ctx, []string{"--supervise", "--retire-on-stop"}, state, seat, true, child, io.Discard)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(marker); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("supervised bridge child did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case rc := <-result:
		if rc != 0 {
			t.Fatalf("supervisor stop rc=%d", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop and retire")
	}

	journal, err = OpenJournal(filepath.Join(SeatDir(state, seat), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	pending, retired := journal.Counts()
	if pending != 0 || retired != 1 || journal.receipts[91].Status() != "undeliverable" {
		t.Fatalf("pending=%d retired=%d receipt=%s", pending, retired, journal.receipts[91].Status())
	}
}

func seedGrokRegistryRow(t *testing.T, state, seat, sessionID string) {
	t.Helper()
	path := filepath.Join(state, "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{
			{
				Kind: v2.KindSession, GUID: seat, Event: "registered", RecordedAt: "2026-07-13T00:00:00Z", State: v2.StateSeated,
				Label: "grok-seat", Role: "worker", Tool: "grok", SIDs: []v2.SID{{SID: sessionID}}, Provenance: v2.Provenance{ToolSessionID: sessionID},
				Seat: &v2.Seat{Kind: "herdr", TerminalID: "terminal-grok", PaneID: "pane-grok", CredentialGeneration: "generation-grok"},
			},
			{
				Kind: v2.KindSession, GUID: "foreign-seat", Event: "registered", RecordedAt: "2026-07-13T00:00:01Z", State: v2.StateSeated,
				Label: "foreign", Role: "worker", Tool: "grok", SIDs: []v2.SID{{SID: "foreign-session"}}, Provenance: v2.Provenance{ToolSessionID: "foreign-session"},
			},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func retireGrokRegistryRow(t *testing.T, state, seat string) {
	t.Helper()
	path := filepath.Join(state, "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		latest := registry.V2ByGUID(tx.Projection, seat)
		if latest == nil {
			return nil, errors.New("seeded Grok row missing")
		}
		next := *latest
		next.Event = "retired"
		next.RecordedAt = "2026-07-13T00:00:02Z"
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func waitForWakeCapability(t *testing.T, state, seat, wake string, pending, undeliverable int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		projection, err := v2.LoadFile(filepath.Join(state, "registry.jsonl"), v2.LoadOptions{})
		if err == nil {
			latest := registry.V2ByGUID(projection, seat)
			if latest != nil && latest.Capabilities != nil && latest.Capabilities.Wake == wake && latest.Capabilities.Pending == pending && latest.Capabilities.Undeliverable == undeliverable {
				if latest.State == v2.StateSeated && (latest.Seat == nil || latest.Seat.CredentialGeneration != "generation-grok") {
					t.Fatalf("capability publication stripped credential generation: %+v", latest.Seat)
				}
				if wake == "down" && (latest.Capabilities.Bus != "" || latest.Capabilities.BinderPID != 0) {
					t.Fatalf("down capability retained live claims: %+v", latest.Capabilities)
				}
				if wake != "down" && (latest.Capabilities.Bus != "bound" || latest.Capabilities.BinderPID <= 0) {
					t.Fatalf("live capability omitted bridge claims: %+v", latest.Capabilities)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("capability did not reach wake=%s pending=%d undeliverable=%d", wake, pending, undeliverable)
		}
		time.Sleep(time.Millisecond)
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
	script := "#!/bin/sh\nif [ \"$1\" = list ]; then printf '%s\\n' '{\"name\":\"bound\"}'; exit 0; fi\nif [ \"$1\" = events ]; then\n  case \" $* \" in *\" --wait \"*) sleep 10; exit 1;; *) exit 0;; esac\nfi\nexit 0\n"
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
