package servecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// The herdr view: one herdr client per WebSocket, in a PTY the serve owns.
// The session, its server and its panes are herdr's; closing the socket ends
// only this client (see docs/web-api-contract.md, herdr view amendment).

const (
	herdrTermPath = "/api/herdr/term"
	// HerdrTermCommandEnv replaces the herdr client with `sh -c <value>`;
	// tests and scratch serves use it so nothing attaches to the real session.
	HerdrTermCommandEnv = "HERDER_TERM_COMMAND"
	termReadLimit       = 1 << 20
	termHangupGrace     = time.Second
	termMinSize         = 2
	termMaxSize         = 1000
)

// termControl is one text frame from the browser: a resize, or a request
// to focus a pane in herdr (the "follow the current agent" switch).
type termControl struct {
	Type   string `json:"type"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	PaneID string `json:"pane_id,omitempty"`
}

// termNotice is one text frame to the browser.
type termNotice struct {
	Type   string `json:"type"`
	PaneID string `json:"pane_id,omitempty"`
	Detail string `json:"detail,omitempty"`
	Code   *int   `json:"code,omitempty"`
}

// liveTermArgv is the herdr client the view runs: the same `herdr` on PATH
// herder already drives, attaching to the default persistent session.
func liveTermArgv() []string {
	if command := strings.TrimSpace(os.Getenv(HerdrTermCommandEnv)); command != "" {
		return []string{"/bin/sh", "-c", command}
	}
	return []string{"herdr"}
}

// strippedTermEnv names the inherited variables a herdr client must not
// see. HERDR_ENV/HERDR_PANE_ID make herdr refuse a nested launch (unless
// experimental.allow_nested) and would retarget its CLI at the serve's own
// pane; the outer terminal's identity would steer Ghostty/kitty-specific
// output at a browser that is neither.
func strippedTermEnv(key string) bool {
	if strings.HasPrefix(key, "HERDR_") || strings.HasPrefix(key, "GHOSTTY_") || strings.HasPrefix(key, "KITTY_") || strings.HasPrefix(key, "WEZTERM_") {
		return true
	}
	switch key {
	case "TERM", "COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "TMUX", "TMUX_PANE", "STY", "VTE_VERSION", "LC_TERMINAL", "LC_TERMINAL_VERSION", "COLUMNS", "LINES":
		return true
	}
	return false
}

func termEnv(environ []string) []string {
	env := make([]string, 0, len(environ)+2)
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if !strippedTermEnv(key) {
			env = append(env, entry)
		}
	}
	// xterm.js speaks xterm-256color with 24-bit colour.
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
}

// sameOrigin is true when the browser's Origin names this serve's own host.
// A missing Origin is refused: browsers always send one on a WebSocket
// upgrade, and only a non-browser client omits it.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func termSize(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return max(termMinSize, min(termMaxSize, value))
}

func serveHerdrTerm(w http.ResponseWriter, r *http.Request, deps dependencies) {
	if r.Method != http.MethodGet {
		refuse(w, http.StatusBadRequest, "bad request", "GET required")
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		refuse(w, http.StatusBadRequest, "bad request", "WebSocket upgrade required")
		return
	}
	if !sameOrigin(r) {
		refuse(w, http.StatusForbidden, "origin refused", "the herdr view only accepts this serve's own page as Origin")
		return
	}
	roster, err := deps.fleet().Roster()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	viewer, ok := webSender(w, r, deps, roster)
	if !ok {
		return
	}
	if deps.termArgv == nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", "herdr view command is not configured")
		return
	}
	argv := deps.termArgv()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = termEnv(os.Environ())
	size := &pty.Winsize{Cols: uint16(termSize(r.URL.Query().Get("cols"), 80)), Rows: uint16(termSize(r.URL.Query().Get("rows"), 24))}
	// pty.Start makes the client a session (and process group) leader with
	// the PTY as its controlling terminal, so one group signal reaches all of it.
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", "start herdr client: "+err.Error())
		return
	}
	exited := make(chan struct{})
	var exitCode int
	go func() {
		_ = cmd.Wait()
		exitCode = cmd.ProcessState.ExitCode()
		close(exited)
	}()
	defer func() {
		endTermGroup(cmd.Process.Pid, exited)
		_ = ptmx.Close()
	}()

	// Origin is already checked above (strictly: present and same host).
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(termReadLimit)
	deps.audit("herdr_term open time=%s viewer=%s pid=%d", deps.now().UTC().Format(time.RFC3339Nano), viewer, cmd.Process.Pid)
	defer deps.audit("herdr_term close time=%s viewer=%s pid=%d", deps.now().UTC().Format(time.RFC3339Nano), viewer, cmd.Process.Pid)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var writes sync.Mutex
	send := func(kind websocket.MessageType, data []byte) error {
		writes.Lock()
		defer writes.Unlock()
		return conn.Write(ctx, kind, data)
	}
	notice := func(value termNotice) {
		data, _ := json.Marshal(value)
		_ = send(websocket.MessageText, data)
	}

	// PTY → socket. EIO on read means the client process is gone.
	output := make(chan struct{})
	go func() {
		defer close(output)
		buffer := make([]byte, 32<<10)
		for {
			n, err := ptmx.Read(buffer)
			if n > 0 {
				if send(websocket.MessageBinary, buffer[:n]) != nil {
					cancel()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		select {
		case <-output:
		case <-ctx.Done():
			return
		}
		select {
		case <-exited:
		case <-time.After(termHangupGrace):
		case <-ctx.Done():
			return
		}
		select {
		case <-exited:
			code := exitCode
			notice(termNotice{Type: "exit", Code: &code})
		default:
			notice(termNotice{Type: "exit"})
		}
		_ = conn.Close(websocket.StatusNormalClosure, "herdr client exited")
		cancel()
	}()

	// Socket → PTY: binary frames are keystrokes, text frames are control.
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind == websocket.MessageBinary {
			if _, err := ptmx.Write(data); err != nil {
				return
			}
			continue
		}
		var control termControl
		if err := json.Unmarshal(data, &control); err != nil {
			notice(termNotice{Type: "error", Detail: "control frames must be JSON"})
			continue
		}
		switch control.Type {
		case "resize":
			if control.Cols < termMinSize || control.Rows < termMinSize || control.Cols > termMaxSize || control.Rows > termMaxSize {
				notice(termNotice{Type: "error", Detail: fmt.Sprintf("resize must be %d..%d columns and rows", termMinSize, termMaxSize)})
				continue
			}
			_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(control.Cols), Rows: uint16(control.Rows)})
		case "focus":
			if err := focusHerdrPane(deps, control.PaneID); err != nil {
				notice(termNotice{Type: "focus_failed", PaneID: control.PaneID, Detail: err.Error()})
				continue
			}
			deps.audit("herdr_term focus time=%s viewer=%s pane=%s", deps.now().UTC().Format(time.RFC3339Nano), viewer, control.PaneID)
			notice(termNotice{Type: "focused", PaneID: control.PaneID})
		default:
			notice(termNotice{Type: "error", Detail: fmt.Sprintf("unknown control %q", control.Type)})
		}
	}
}

// focusHerdrPane asks herdr to show a pane the current snapshot reports.
func focusHerdrPane(deps dependencies, paneID string) error {
	if paneID == "" {
		return errors.New("pane_id is required")
	}
	snapshot, err := deps.snapshot()
	if err != nil {
		return err
	}
	found := false
	for _, pane := range snapshot.Panes {
		if pane.PaneID == paneID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("pane %q is not reported by Herdr", paneID)
	}
	if deps.focusPane == nil {
		return errors.New("herdr focus is not configured")
	}
	return deps.focusPane(paneID)
}

// endTermGroup hangs up the client's process group, kills whatever is
// left after a short grace, and waits until the client is reaped.
func endTermGroup(pid int, exited <-chan struct{}) {
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	select {
	case <-exited:
	case <-time.After(termHangupGrace):
	}
	// Sweep members that outlived the leader or ignored the hangup.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	<-exited
}
