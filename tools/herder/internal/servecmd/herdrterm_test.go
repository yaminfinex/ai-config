package servecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"ai-config/tools/herder/internal/webidentity"
	"github.com/coder/websocket"
)

// Every test here runs a fake client command; nothing attaches to herdr.

type termFixture struct {
	server   *httptest.Server
	starts   atomic.Int32
	mu       sync.Mutex
	focused  []string
	audits   []string
	focusErr error
}

func newTermFixture(t *testing.T, script string, mutate ...func(*dependencies)) *termFixture {
	t.Helper()
	fixture := &termFixture{}
	deps := fixtureDeps()
	deps.termArgv = func() []string {
		fixture.starts.Add(1)
		return []string{"/bin/sh", "-c", script}
	}
	deps.focusPane = func(paneID string) error {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		if fixture.focusErr != nil {
			return fixture.focusErr
		}
		fixture.focused = append(fixture.focused, paneID)
		return nil
	}
	deps.audit = func(format string, args ...any) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		fixture.audits = append(fixture.audits, format)
	}
	for _, change := range mutate {
		change(&deps)
	}
	fixture.server = httptest.NewServer(newDrainingHandler(newHandler(deps)))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *termFixture) url(query string) string {
	return "ws" + strings.TrimPrefix(f.server.URL, "http") + herdrTermPath + query
}

func (f *termFixture) dial(t *testing.T, query string) *termClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, response, err := websocket.Dial(ctx, f.url(query), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {f.server.URL}}})
	if err != nil {
		t.Fatalf("dial: %v (response %v)", err, response)
	}
	client := &termClient{conn: conn, ctx: ctx, notices: make(chan termNotice, 16), output: make(chan struct{}, 1)}
	go client.pump()
	t.Cleanup(func() { conn.CloseNow() })
	return client
}

type termClient struct {
	conn    *websocket.Conn
	ctx     context.Context
	mu      sync.Mutex
	buffer  bytes.Buffer
	binary  bool
	notices chan termNotice
	output  chan struct{}
	closed  atomic.Bool
}

func (c *termClient) pump() {
	defer c.closed.Store(true)
	for {
		kind, data, err := c.conn.Read(c.ctx)
		if err != nil {
			close(c.notices)
			return
		}
		if kind == websocket.MessageText {
			var notice termNotice
			_ = json.Unmarshal(data, &notice)
			c.notices <- notice
			continue
		}
		c.mu.Lock()
		c.binary = true
		c.buffer.Write(data)
		c.mu.Unlock()
		select {
		case c.output <- struct{}{}:
		default:
		}
	}
}

func (c *termClient) waitOutput(t *testing.T, want func([]byte) bool) []byte {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		c.mu.Lock()
		seen := append([]byte(nil), c.buffer.Bytes()...)
		c.mu.Unlock()
		if want(seen) {
			return seen
		}
		select {
		case <-c.output:
		case <-deadline:
			t.Fatalf("output never matched; saw %q", seen)
		}
	}
}

func (c *termClient) waitText(t *testing.T, text string) []byte {
	t.Helper()
	return c.waitOutput(t, func(seen []byte) bool { return bytes.Contains(seen, []byte(text)) })
}

func (c *termClient) send(t *testing.T, kind websocket.MessageType, data string) {
	t.Helper()
	if err := c.conn.Write(c.ctx, kind, []byte(data)); err != nil {
		t.Fatal(err)
	}
}

func (c *termClient) notice(t *testing.T) termNotice {
	t.Helper()
	select {
	case notice, ok := <-c.notices:
		if !ok {
			t.Fatal("socket closed before a notice")
		}
		return notice
	case <-time.After(5 * time.Second):
		t.Fatal("no notice")
	}
	return termNotice{}
}

func gone(pid int) bool {
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return true
	}
	// A zombie still answers kill(0); it is dead, only unreaped.
	status, readErr := readProcState(pid)
	return readErr == nil && status == "Z"
}

func readProcState(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data[bytes.LastIndexByte(data, ')')+1:]))
	if len(fields) == 0 {
		return "", errors.New("short stat")
	}
	return fields[0], nil
}

func waitGone(t *testing.T, pids ...int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for !gone(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("pid %d outlived the closed socket", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestHerdrTermEchoesInputRoundTrip(t *testing.T) {
	fixture := newTermFixture(t, `stty -echo; printf 'ready\n'; cat`)
	client := fixture.dial(t, "")
	client.waitText(t, "ready")
	client.send(t, websocket.MessageBinary, "ping-42\n")
	client.waitText(t, "ping-42")
	if !client.binary {
		t.Fatal("PTY output must arrive as binary frames")
	}
}

func TestHerdrTermOutputIsBinarySafe(t *testing.T) {
	fixture := newTermFixture(t, `stty raw -echo; printf '\000\001\377\376\033[31mred|end'; cat`)
	client := fixture.dial(t, "")
	seen := client.waitText(t, "|end")
	want := []byte{0, 1, 0xff, 0xfe, 0x1b, '[', '3', '1', 'm', 'r', 'e', 'd', '|', 'e', 'n', 'd'}
	if !bytes.Contains(seen, want) {
		t.Fatalf("binary output = %v, want it to contain %v", seen, want)
	}
}

func TestHerdrTermResizeReachesThePTY(t *testing.T) {
	fixture := newTermFixture(t, `stty -echo; printf 'size %s\n' "$(stty size)"; while read line; do printf 'size %s\n' "$(stty size)"; done`)
	client := fixture.dial(t, "?cols=100&rows=30")
	client.waitText(t, "size 30 100")
	client.send(t, websocket.MessageText, `{"type":"resize","cols":132,"rows":43}`)
	client.send(t, websocket.MessageBinary, "again\n")
	client.waitText(t, "size 43 132")

	client.send(t, websocket.MessageText, `{"type":"resize","cols":0,"rows":43}`)
	if notice := client.notice(t); notice.Type != "error" || !strings.Contains(notice.Detail, "resize") {
		t.Fatalf("bad resize notice = %#v", notice)
	}
}

func TestHerdrTermCloseEndsTheWholeProcessGroup(t *testing.T) {
	// A background child that ignores the hangup proves the SIGKILL sweep.
	fixture := newTermFixture(t, `stty -echo; sleep 300 & a=$!; (trap '' HUP; exec sleep 301) & b=$!; printf 'pids %s %s %s\n' "$$" "$a" "$b"; cat`)
	client := fixture.dial(t, "")
	seen := client.waitOutput(t, func(seen []byte) bool { return regexp.MustCompile(`pids \d+ \d+ \d+\r?\n`).Match(seen) })
	match := regexp.MustCompile(`pids (\d+) (\d+) (\d+)`).FindSubmatch(seen)
	var pids []int
	for _, raw := range match[1:] {
		pid, _ := strconv.Atoi(string(raw))
		pids = append(pids, pid)
	}
	for _, pid := range pids {
		if gone(pid) {
			t.Fatalf("pid %d was not running before close", pid)
		}
	}
	if err := client.conn.Close(websocket.StatusNormalClosure, "leaving"); err != nil && !strings.Contains(err.Error(), "close") {
		t.Fatal(err)
	}
	waitGone(t, pids...)
}

func TestHerdrTermReportsTheClientExiting(t *testing.T) {
	fixture := newTermFixture(t, `printf 'bye\n'; exit 3`)
	client := fixture.dial(t, "")
	client.waitText(t, "bye")
	notice := client.notice(t)
	if notice.Type != "exit" || notice.Code == nil || *notice.Code != 3 {
		t.Fatalf("exit notice = %#v", notice)
	}
}

func TestHerdrTermStripsNestingEnvAndSetsTerm(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w9:p9")
	t.Setenv("HERDR_SOCKET_PATH", "/nowhere/herdr.sock")
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "/nowhere")
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("HERDER_KEEP_ME", "kept")
	fixture := newTermFixture(t, `stty -echo; env | sort; printf 'env-end\n'; cat`)
	client := fixture.dial(t, "")
	seen := string(client.waitText(t, "env-end"))
	for _, banned := range []string{"HERDR_", "TERM_PROGRAM=", "GHOSTTY_", "TMUX="} {
		if strings.Contains(seen, banned) {
			t.Fatalf("env leaked %s:\n%s", banned, seen)
		}
	}
	for _, kept := range []string{"TERM=xterm-256color", "COLORTERM=truecolor", "HERDER_KEEP_ME=kept"} {
		if !strings.Contains(seen, kept) {
			t.Fatalf("env misses %s:\n%s", kept, seen)
		}
	}
}

func TestHerdrTermEnvKeepsOneTerm(t *testing.T) {
	env := termEnv([]string{"PATH=/bin", "TERM=dumb", "HERDR_TAB_ID=t1", "HERDR_PLUGIN_ID=x", "COLUMNS=80", "HERDERISH=1"})
	want := []string{"PATH=/bin", "HERDERISH=1", "TERM=xterm-256color", "COLORTERM=truecolor"}
	if !slices.Equal(env, want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
}

func TestHerdrTermFollowFocusesALivePaneOnly(t *testing.T) {
	fixture := newTermFixture(t, `cat`)
	client := fixture.dial(t, "")
	client.send(t, websocket.MessageText, `{"type":"focus","pane_id":"p1"}`)
	if notice := client.notice(t); notice.Type != "focused" || notice.PaneID != "p1" {
		t.Fatalf("focus notice = %#v", notice)
	}
	client.send(t, websocket.MessageText, `{"type":"focus","pane_id":"w9:p404"}`)
	if notice := client.notice(t); notice.Type != "focus_failed" || !strings.Contains(notice.Detail, "not reported by Herdr") {
		t.Fatalf("unknown pane notice = %#v", notice)
	}
	fixture.mu.Lock()
	fixture.focusErr = errors.New("herdr pane.focus: boom: refused")
	fixture.mu.Unlock()
	client.send(t, websocket.MessageText, `{"type":"focus","pane_id":"p1"}`)
	if notice := client.notice(t); notice.Type != "focus_failed" || !strings.Contains(notice.Detail, "boom") {
		t.Fatalf("refused focus notice = %#v", notice)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if !slices.Equal(fixture.focused, []string{"p1"}) {
		t.Fatalf("focused = %v", fixture.focused)
	}
}

func TestHerdrTermRefusesBeforeStartingAnything(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*dependencies)
		header http.Header
		method string
		status int
		error  string
	}{
		{name: "plain GET", header: http.Header{"Origin": {"SELF"}}, method: http.MethodGet, status: http.StatusBadRequest, error: "bad request"},
		{name: "POST", header: http.Header{"Origin": {"SELF"}, "Upgrade": {"websocket"}}, method: http.MethodPost, status: http.StatusBadRequest, error: "bad request"},
		{name: "no origin", header: http.Header{"Upgrade": {"websocket"}}, method: http.MethodGet, status: http.StatusForbidden, error: "origin refused"},
		{name: "foreign origin", header: http.Header{"Origin": {"http://evil.example"}, "Upgrade": {"websocket"}}, method: http.MethodGet, status: http.StatusForbidden, error: "origin refused"},
		{name: "file origin", header: http.Header{"Origin": {"null"}, "Upgrade": {"websocket"}}, method: http.MethodGet, status: http.StatusForbidden, error: "origin refused"},
		{name: "unattributed", mutate: func(deps *dependencies) {
			deps.sender = func(context.Context, string) (string, error) {
				return "", errors.New("tailscale whois failed: peer not found")
			}
		}, header: http.Header{"Origin": {"SELF"}, "Upgrade": {"websocket"}}, method: http.MethodGet, status: http.StatusConflict, error: "attribution required"},
		{name: "tailscale down", mutate: func(deps *dependencies) {
			deps.sender = func(context.Context, string) (string, error) {
				return "", fmt.Errorf("%w: tailscale whois timed out", webidentity.ErrUnavailable)
			}
		}, header: http.Header{"Origin": {"SELF"}, "Upgrade": {"websocket"}}, method: http.MethodGet, status: http.StatusBadGateway, error: "substrate unreachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mutate []func(*dependencies)
			if tc.mutate != nil {
				mutate = append(mutate, tc.mutate)
			}
			fixture := newTermFixture(t, `cat`, mutate...)
			request, _ := http.NewRequest(tc.method, fixture.server.URL+herdrTermPath, nil)
			for key, values := range tc.header {
				for _, value := range values {
					if value == "SELF" {
						value = fixture.server.URL
					}
					request.Header.Add(key, value)
				}
			}
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Sec-WebSocket-Version", "13")
			request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body refusal
			_ = json.NewDecoder(response.Body).Decode(&body)
			if response.StatusCode != tc.status || body.Error != tc.error {
				t.Fatalf("status=%d body=%#v, want %d %q", response.StatusCode, body, tc.status, tc.error)
			}
			if starts := fixture.starts.Load(); starts != 0 {
				t.Fatalf("a refused request started %d client(s)", starts)
			}
		})
	}
}

func TestHerdrTermEndsOnDrainAndStop(t *testing.T) {
	deps := fixtureDeps()
	deps.termArgv = func() []string { return []string{"/bin/sh", "-c", `stty -echo; printf 'pid %s\n' "$$"; cat`} }
	drainer := newDrainingHandler(newHandler(deps))
	server := httptest.NewServer(drainer)
	defer server.Close()
	fixture := &termFixture{server: server}
	for _, end := range []func(){
		func() { drainer.hangUpTerms(3 * termHangupGrace) },
		func() { <-drainer.beginDrain() },
	} {
		client := fixture.dial(t, "")
		seen := client.waitOutput(t, func(seen []byte) bool { return regexp.MustCompile(`pid \d+\r?\n`).Match(seen) })
		pid, _ := strconv.Atoi(string(regexp.MustCompile(`pid (\d+)`).FindSubmatch(seen)[1]))
		end()
		waitGone(t, pid)
		drainer.mu.Lock()
		drainer.draining = false
		drainer.mu.Unlock()
	}
}
