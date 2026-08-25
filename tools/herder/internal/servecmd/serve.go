// Package servecmd exposes the read-only fleet web API.
package servecmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/hcommessage"
	"ai-config/tools/herder/internal/hcomtranscript"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/webaction"
	"ai-config/tools/herder/internal/webidentity"
	"ai-config/tools/herder/internal/webui"
)

const (
	DefaultPort = 4400
	PollCadence = 2 * time.Second
)

type dependencies struct {
	snapshot   func() (herdrcli.Snapshot, error)
	roster     func() ([]hcomidentity.Row, error)
	messages   func(context.Context, *hcomevents.Cursor, func(hcomevents.Message) error, func() error) error
	transcript func(context.Context, string, int, int, hcomtranscript.Detail) ([]hcomtranscript.Exchange, error)
	latest     func(context.Context, string, hcomtranscript.Detail) (hcomtranscript.Exchange, bool, error)
	rangeRead  func(context.Context, string, int, int, hcomtranscript.Detail) ([]hcomtranscript.Exchange, error)
	sender     func(context.Context, string) (string, error)
	send       func(context.Context, string, string, string) error
	spawn      func(context.Context, []string) (webaction.Result, error)
	fork       func(context.Context, string, string, bool) (string, error)
	poll       time.Duration
	listeners  func(int) ([]net.Listener, []string, error)
}

var liveDependencies = dependencies{
	snapshot:   herdrcli.LiveSnapshot,
	roster:     hcomidentity.List,
	messages:   hcomevents.Subscribe,
	transcript: hcomtranscript.Window,
	latest:     hcomtranscript.Latest,
	rangeRead:  hcomtranscript.Range,
	sender:     webidentity.Sender,
	send:       hcommessage.SendRequest,
	spawn:      webaction.Spawn,
	fork:       webaction.Fork,
	poll:       PollCadence,
	listeners:  liveListeners,
}

type refusal struct {
	Error  string `json:"error"`
	Detail string `json:"detail"`
}

type substrate struct {
	Source string `json:"source"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type sourceError struct {
	source string
	err    error
}

func (e sourceError) Error() string { return e.err.Error() }

type agentPane struct {
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	PaneID      string `json:"pane_id"`
}

type agentDetail struct {
	Name          string                     `json:"name"`
	Tool          string                     `json:"tool"`
	HerdrStatus   string                     `json:"herdr_status"`
	BusStatus     string                     `json:"bus_status"`
	Gap           string                     `json:"gap"`
	Pane          *agentPane                 `json:"pane"`
	Directory     string                     `json:"directory,omitempty"`
	SessionID     string                     `json:"session_id,omitempty"`
	LaunchContext hcomidentity.LaunchContext `json:"launch_context"`
}

type transcriptPage struct {
	Exchanges []hcomtranscript.Exchange `json:"exchanges"`
	Cursor    string                    `json:"cursor"`
}

type transcriptCursor struct {
	Version  int                   `json:"v"`
	Kind     string                `json:"kind"`
	Agent    string                `json:"agent"`
	Session  string                `json:"session,omitempty"`
	Detail   hcomtranscript.Detail `json:"detail"`
	Position int                   `json:"position"`
}

type messageRequest struct {
	Text string `json:"text"`
}

type messageResponse struct {
	Sent   bool   `json:"sent"`
	To     string `json:"to"`
	From   string `json:"from"`
	Intent string `json:"intent"`
}

type spawnRequest struct {
	FromPane *string `json:"from_pane"`
	Shape    *string `json:"shape"`
	Tool     *string `json:"tool"`
	Tag      *string `json:"tag"`
	Prompt   *string `json:"prompt"`
	Branch   *string `json:"branch"`
}

type forkRequest struct {
	Prompt *string `json:"prompt"`
}

var (
	tagPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// Run parses and serves `herder serve` until the process is stopped.
func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("herder serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", DefaultPort, "TCP port for loopback and tailscale listeners")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "herder serve — expose the read-only live fleet API.\n\nUsage:\n  herder serve [--port PORT]\n")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "herder serve: unknown argument %q\n", fs.Arg(0))
		return 2
	}
	if *port < 1 || *port > 65535 {
		fmt.Fprintf(stderr, "herder serve: invalid port %d\n", *port)
		return 2
	}
	listeners, warnings, err := liveDependencies.listeners(*port)
	if err != nil {
		fmt.Fprintf(stderr, "herder serve: %v\n", err)
		return 1
	}
	for _, warning := range warnings {
		fmt.Fprintf(stderr, "herder serve: WARNING: %s\n", warning)
	}
	return serve(listeners, newHandler(liveDependencies), stdout, stderr)
}

func serve(listeners []net.Listener, handler http.Handler, stdout, stderr io.Writer) int {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, len(listeners))
	for _, listener := range listeners {
		fmt.Fprintf(stdout, "herder serve: listening on http://%s\n", listener.Addr())
		go func(l net.Listener) { errCh <- server.Serve(l) }(listener)
	}
	select {
	case <-ctx.Done():
		// SSE connections are intentionally long-lived, so close immediately
		// instead of waiting for graceful drain. Closing cancels their request
		// contexts and reaps the blocking hcom subscription subprocesses.
		_ = server.Close()
		return 0
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return 0
		}
		fmt.Fprintf(stderr, "herder serve: server failed: %v\n", err)
		_ = server.Close()
		return 1
	}
}

func liveListeners(port int) ([]net.Listener, []string, error) {
	addresses := []string{"127.0.0.1"}
	warnings := []string{}
	// Hermetic integration tests explicitly disable host interface discovery.
	// This does not alter the public flag surface or permit a wildcard bind.
	if os.Getenv("HERDER_SERVE_TEST_LOOPBACK_ONLY") == "" {
		iface, err := net.InterfaceByName("tailscale0")
		if err != nil {
			warnings = append(warnings, "tailscale0 is unavailable; serving loopback only")
		} else {
			addrs, addrErr := iface.Addrs()
			if addrErr != nil {
				return nil, nil, fmt.Errorf("read tailscale0 addresses: %w", addrErr)
			}
			for _, addr := range addrs {
				host := strings.SplitN(addr.String(), "/", 2)[0]
				if host != "" {
					addresses = append(addresses, host)
				}
			}
			if len(addresses) == 1 {
				warnings = append(warnings, "tailscale0 has no bindable address; serving loopback only")
			}
		}
	} else {
		warnings = append(warnings, "test-scoped loopback-only binding is active")
	}
	listeners, bindWarnings, err := openListeners(addresses, port)
	warnings = append(warnings, bindWarnings...)
	return listeners, warnings, err
}

func openListeners(addresses []string, port int) ([]net.Listener, []string, error) {
	listeners := make([]net.Listener, 0, len(addresses))
	warnings := []string{}
	for _, address := range addresses {
		ip := net.ParseIP(address)
		if ip == nil || ip.IsUnspecified() {
			for _, open := range listeners {
				_ = open.Close()
			}
			return nil, nil, fmt.Errorf("refusing unspecified or invalid bind address %q", address)
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(address, strconv.Itoa(port)))
		if err != nil {
			if address == "127.0.0.1" {
				for _, open := range listeners {
					_ = open.Close()
				}
				return nil, nil, fmt.Errorf("bind %s: %w", address, err)
			}
			warnings = append(warnings, fmt.Sprintf("cannot bind tailscale address %s: %v", address, err))
			continue
		}
		listeners = append(listeners, listener)
	}
	return listeners, warnings, nil
}

func newHandler(deps dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/fleet", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		board, err := readBoard(deps)
		if err != nil {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, board)
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveEvents(w, r, deps)
	})
	mux.HandleFunc("/api/agents/{busName}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		detail, err := readAgent(deps, r.PathValue("busName"))
		if err != nil {
			serveAgentReadError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
	mux.HandleFunc("/api/agents/{busName}/transcript", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveTranscriptWindow(w, r, deps, r.PathValue("busName"))
	})
	mux.HandleFunc("/api/agents/{busName}/transcript/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveTranscriptStream(w, r, deps, r.PathValue("busName"))
	})
	mux.HandleFunc("/api/agents/{busName}/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			refuse(w, http.StatusBadRequest, "bad request", "POST required")
			return
		}
		serveMessage(w, r, deps, r.PathValue("busName"))
	})
	mux.HandleFunc("/api/spawn", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			refuse(w, http.StatusBadRequest, "bad request", "POST required")
			return
		}
		serveSpawn(w, r, deps)
	})
	mux.HandleFunc("/api/agents/{busName}/fork", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			refuse(w, http.StatusBadRequest, "bad request", "POST required")
			return
		}
		serveFork(w, r, deps, r.PathValue("busName"))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		refuse(w, http.StatusNotFound, "not found", "unknown endpoint")
	})
	mux.HandleFunc("/api", func(w http.ResponseWriter, _ *http.Request) {
		refuse(w, http.StatusNotFound, "not found", "unknown endpoint")
	})
	ui := http.FileServer(http.FS(webui.Files()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
			mux.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		if webui.Has(r.URL.Path) {
			ui.ServeHTTP(w, r)
			return
		}
		// Non-API routes are SPA routes. Serve index.html without redirecting
		// so a future client-side route can be opened directly.
		request := r.Clone(r.Context())
		request.URL.Path = "/"
		ui.ServeHTTP(w, request)
	})
}

func readBoard(deps dependencies) (fleetview.Board, error) {
	snapshot, roster, err := readFleetInputs(deps)
	if err != nil {
		return fleetview.Board{}, err
	}
	return fleetview.Build(snapshot, roster), nil
}

func readFleetInputs(deps dependencies) (herdrcli.Snapshot, []hcomidentity.Row, error) {
	snapshot, err := deps.snapshot()
	if err != nil {
		return herdrcli.Snapshot{}, nil, sourceError{"herdr", err}
	}
	if err := fleetview.ValidateSnapshot(snapshot); err != nil {
		return herdrcli.Snapshot{}, nil, sourceError{"herdr", fmt.Errorf("invalid session hierarchy: %w", err)}
	}
	roster, err := deps.roster()
	if err != nil {
		return herdrcli.Snapshot{}, nil, sourceError{"hcom", err}
	}
	if err := fleetview.ValidateRoster(roster); err != nil {
		return herdrcli.Snapshot{}, nil, sourceError{"hcom", fmt.Errorf("invalid roster: %w", err)}
	}
	return snapshot, roster, nil
}

var errUnknownAgent = errors.New("unknown agent")

func readAgent(deps dependencies, name string) (agentDetail, error) {
	snapshot, roster, err := readFleetInputs(deps)
	if err != nil {
		return agentDetail{}, err
	}
	var bus *hcomidentity.Row
	for i := range roster {
		if roster[i].Name == name {
			bus = &roster[i]
			break
		}
	}
	if bus == nil {
		return agentDetail{}, fmt.Errorf("%w: agent %q is not on the hcom bus", errUnknownAgent, name)
	}
	result := agentDetail{
		Name: name, Tool: bus.Tool, HerdrStatus: "-", BusStatus: bus.Status,
		Gap: "no visible pane", Directory: bus.Directory, SessionID: bus.SessionID,
		LaunchContext: bus.LaunchContext,
	}
	for _, row := range fleetview.JoinRows(snapshot, roster) {
		if row.Agent == name && (row.Pane == bus.LaunchContext.PaneID || row.Pane == "-") {
			result.Tool = row.Tool
			result.HerdrStatus = row.HerdrStatus
			result.BusStatus = row.BusStatus
			result.Gap = row.Gap
			break
		}
	}
	if bus.LaunchContext.PaneID != "" {
		for _, pane := range snapshot.Panes {
			if pane.PaneID == bus.LaunchContext.PaneID {
				result.Pane = &agentPane{WorkspaceID: pane.WorkspaceID, TabID: pane.TabID, PaneID: pane.PaneID}
				break
			}
		}
	}
	return result, nil
}

func serveAgentReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnknownAgent) {
		refuse(w, http.StatusNotFound, "unknown agent", err.Error())
		return
	}
	refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
}

func transcriptDetail(value string) (hcomtranscript.Detail, error) {
	if value == "" || value == string(hcomtranscript.Exchanges) {
		return hcomtranscript.Exchanges, nil
	}
	if value == string(hcomtranscript.Full) {
		return hcomtranscript.Full, nil
	}
	return "", fmt.Errorf("detail must be %q or %q", hcomtranscript.Exchanges, hcomtranscript.Full)
}

func serveTranscriptWindow(w http.ResponseWriter, r *http.Request, deps dependencies, name string) {
	agent, err := readAgent(deps, name)
	if err != nil {
		serveAgentReadError(w, err)
		return
	}
	detail, err := transcriptDetail(r.URL.Query().Get("detail"))
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			refuse(w, http.StatusBadRequest, "bad request", "limit must be an integer from 1 through 100")
			return
		}
	}
	before := 0
	if raw := r.URL.Query().Get("before"); raw != "" {
		cursor, cursorErr := decodeTranscriptCursor(raw, "page", name, agent.SessionID, detail)
		if cursorErr != nil {
			refuse(w, http.StatusBadRequest, "bad request", cursorErr.Error())
			return
		}
		before = cursor.Position
	}
	exchanges, err := deps.transcript(r.Context(), name, before, limit, detail)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	next := before
	if len(exchanges) > 0 {
		next = exchanges[0].Position
	} else if next == 0 {
		next = 1
	}
	writeJSON(w, http.StatusOK, transcriptPage{Exchanges: exchanges, Cursor: encodeTranscriptCursor(transcriptCursor{
		Version: 1, Kind: "page", Agent: name, Session: agent.SessionID, Detail: detail, Position: next,
	})})
}

func encodeTranscriptCursor(cursor transcriptCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeTranscriptCursor(raw, kind, agent, session string, detail hcomtranscript.Detail) (transcriptCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return transcriptCursor{}, errors.New("invalid transcript cursor")
	}
	var cursor transcriptCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Version != 1 || cursor.Kind != kind || cursor.Agent != agent || cursor.Session != session || cursor.Detail != detail || cursor.Position < 0 {
		return transcriptCursor{}, errors.New("invalid transcript cursor")
	}
	return cursor, nil
}

func serveTranscriptStream(w http.ResponseWriter, r *http.Request, deps dependencies, name string) {
	agent, err := readAgent(deps, name)
	if err != nil {
		serveAgentReadError(w, err)
		return
	}
	detail, err := transcriptDetail(r.URL.Query().Get("detail"))
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	position := 0
	resuming := false
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		cursor, err := decodeTranscriptCursor(raw, "stream", name, agent.SessionID, detail)
		if err != nil {
			refuse(w, http.StatusBadRequest, "bad request", err.Error())
			return
		}
		position = cursor.Position
		resuming = true
	}
	latest, ok, err := deps.latest(r.Context(), name, detail)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !resuming && ok {
		position = latest.Position
	}
	flusher, okFlusher := w.(http.Flusher)
	if !okFlusher {
		refuse(w, http.StatusInternalServerError, "stream unavailable", "response writer does not support flushing")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	emitThrough := func(end int) bool {
		for position < end {
			chunkEnd := position + 100
			if chunkEnd > end {
				chunkEnd = end
			}
			exchanges, rangeErr := deps.rangeRead(r.Context(), name, position+1, chunkEnd, detail)
			if rangeErr != nil || len(exchanges) == 0 {
				return false
			}
			for _, exchange := range exchanges {
				cursor := encodeTranscriptCursor(transcriptCursor{
					Version: 1, Kind: "stream", Agent: name, Session: agent.SessionID, Detail: detail, Position: exchange.Position,
				})
				data, marshalErr := json.Marshal(exchange)
				if marshalErr != nil {
					return false
				}
				if _, writeErr := fmt.Fprintf(w, "id: %s\nevent: exchange\ndata: %s\n\n", cursor, data); writeErr != nil {
					return false
				}
				position = exchange.Position
				flusher.Flush()
			}
		}
		return true
	}
	if resuming && ok && latest.Position > position && !emitThrough(latest.Position) {
		return
	}
	ticker := time.NewTicker(deps.poll)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			latest, exists, latestErr := deps.latest(r.Context(), name, detail)
			if latestErr != nil {
				return
			}
			if exists && latest.Position > position && !emitThrough(latest.Position) {
				return
			}
		}
	}
}

func serveMessage(w http.ResponseWriter, r *http.Request, deps dependencies, name string) {
	roster, err := deps.roster()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	found := false
	for _, row := range roster {
		if row.Name == name {
			found = true
			break
		}
	}
	if !found {
		refuse(w, http.StatusNotFound, "unknown agent", fmt.Sprintf("agent %q is not on the hcom bus", name))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request messageRequest
	if err := decoder.Decode(&request); err != nil {
		refuse(w, http.StatusBadRequest, "bad request", "body must be JSON object {\"text\":\"...\"}")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		refuse(w, http.StatusBadRequest, "bad request", "body must contain one JSON object")
		return
	}
	if strings.TrimSpace(request.Text) == "" {
		refuse(w, http.StatusBadRequest, "bad request", "text must not be empty")
		return
	}
	sender, err := deps.sender(r.Context(), r.RemoteAddr)
	if err != nil {
		if errors.Is(err, webidentity.ErrUnavailable) {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
			return
		}
		refuse(w, http.StatusConflict, "attribution required", err.Error())
		return
	}
	for _, row := range roster {
		if strings.EqualFold(row.Name, sender) {
			refuse(w, http.StatusConflict, "sender refused", fmt.Sprintf("derived sender %q is already a bus agent", sender))
			return
		}
	}
	if err := deps.send(r.Context(), name, sender, request.Text); err != nil {
		if errors.Is(err, hcommessage.ErrUnavailable) {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
			return
		}
		refuse(w, http.StatusConflict, "refused by substrate", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, messageResponse{Sent: true, To: name, From: sender, Intent: "request"})
}

func serveSpawn(w http.ResponseWriter, r *http.Request, deps dependencies) {
	var request spawnRequest
	if err := decodeWriteBody(w, r, &request, false); err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if request.FromPane == nil || strings.TrimSpace(*request.FromPane) == "" || request.Shape == nil || request.Tool == nil || request.Tag == nil || request.Prompt == nil {
		refuse(w, http.StatusBadRequest, "bad request", "from_pane, shape, tool, tag, and prompt are required")
		return
	}
	if *request.Shape != "pane" && *request.Shape != "tab" && *request.Shape != "worktree" {
		refuse(w, http.StatusBadRequest, "bad request", `shape must be "pane", "tab", or "worktree"`)
		return
	}
	if *request.Tool != "claude" && *request.Tool != "codex" {
		refuse(w, http.StatusBadRequest, "bad request", "tool must be claude or codex")
		return
	}
	if !tagPattern.MatchString(*request.Tag) {
		refuse(w, http.StatusBadRequest, "bad request", "fleet spawn: --tag must contain only letters, digits, underscore, and hyphen")
		return
	}
	if *request.Shape == "worktree" {
		if request.Branch == nil || !branchPattern.MatchString(*request.Branch) {
			refuse(w, http.StatusBadRequest, "bad request", "fleet spawn: branch must start with a letter or digit and contain only letters, digits, dot, underscore, slash, and hyphen")
			return
		}
	} else if request.Branch != nil {
		refuse(w, http.StatusBadRequest, "bad request", "branch is allowed only for worktree shape")
		return
	}
	roster, err := deps.roster()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if _, err := attributedSender(r, deps, roster); err != nil {
		serveAttributionError(w, err)
		return
	}
	snapshot, err := deps.snapshot()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	var pane *herdrcli.Pane
	for i := range snapshot.Panes {
		if snapshot.Panes[i].PaneID == *request.FromPane {
			pane = &snapshot.Panes[i]
			break
		}
	}
	if pane == nil {
		refuse(w, http.StatusConflict, "refused by substrate", fmt.Sprintf("fleet spawn: pane does not exist: %s", *request.FromPane))
		return
	}
	args := []string{*request.Tool, "--tag", *request.Tag}
	switch *request.Shape {
	case "pane":
		args = append(args, "--split-from", pane.PaneID)
	case "tab":
		args = append(args, "--workspace", pane.WorkspaceID)
	case "worktree":
		var repo string
		for _, workspace := range snapshot.Workspaces {
			if workspace.WorkspaceID == pane.WorkspaceID && workspace.Worktree != nil {
				repo = workspace.Worktree.CheckoutPath
				if repo == "" {
					repo = workspace.Worktree.RepoRoot
				}
				break
			}
		}
		if repo == "" {
			refuse(w, http.StatusConflict, "refused by substrate", fmt.Sprintf("fleet spawn: workspace %s has no repository", pane.WorkspaceID))
			return
		}
		args = append(args, "--worktree-branch", *request.Branch, "--repo", repo)
	}
	args = append(args, "--prompt", *request.Prompt)
	result, err := deps.spawn(r.Context(), args)
	if err != nil {
		if errors.Is(err, webaction.ErrUnavailable) {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		} else {
			refuse(w, http.StatusConflict, "refused by substrate", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func serveFork(w http.ResponseWriter, r *http.Request, deps dependencies, name string) {
	roster, err := deps.roster()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	found := false
	for _, row := range roster {
		if row.Name == name {
			found = true
			break
		}
	}
	if !found {
		refuse(w, http.StatusNotFound, "unknown agent", fmt.Sprintf("agent %q is not on the hcom bus", name))
		return
	}
	var request forkRequest
	if err := decodeWriteBody(w, r, &request, true); err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if _, err := attributedSender(r, deps, roster); err != nil {
		serveAttributionError(w, err)
		return
	}
	prompt := ""
	if request.Prompt != nil {
		prompt = *request.Prompt
	}
	newName, err := deps.fork(r.Context(), name, prompt, request.Prompt != nil)
	if err != nil {
		if errors.Is(err, webaction.ErrUnavailable) {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		} else {
			refuse(w, http.StatusConflict, "refused by substrate", err.Error())
		}
		return
	}
	placement, err := waitForPlacement(r.Context(), deps, newName)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, webaction.Result{Name: newName, Pane: placement})
}

func attributedSender(r *http.Request, deps dependencies, roster []hcomidentity.Row) (string, error) {
	sender, err := deps.sender(r.Context(), r.RemoteAddr)
	if err != nil {
		return "", err
	}
	for _, row := range roster {
		if strings.EqualFold(row.Name, sender) {
			return "", fmt.Errorf("derived sender %q is already a bus agent", sender)
		}
	}
	return sender, nil
}

func serveAttributionError(w http.ResponseWriter, err error) {
	if errors.Is(err, webidentity.ErrUnavailable) {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if strings.Contains(err.Error(), "already a bus agent") {
		refuse(w, http.StatusConflict, "sender refused", err.Error())
		return
	}
	refuse(w, http.StatusConflict, "attribution required", err.Error())
}

func decodeWriteBody(w http.ResponseWriter, r *http.Request, target any, emptyOK bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		if emptyOK && errors.Is(err, io.EOF) {
			return nil
		}
		return errors.New("body must be one JSON object with only documented fields")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' {
		return errors.New("body must be one JSON object with only documented fields")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("body must contain one JSON object")
	}
	objectDecoder := json.NewDecoder(bytes.NewReader(trimmed))
	objectDecoder.DisallowUnknownFields()
	if err := objectDecoder.Decode(target); err != nil {
		return errors.New("body must be one JSON object with only documented fields")
	}
	return nil
}

func waitForPlacement(ctx context.Context, deps dependencies, name string) (string, error) {
	timeout := 2*deps.poll + time.Second
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		snapshot, roster, err := readFleetInputs(deps)
		if err != nil {
			return "", err
		}
		for _, row := range roster {
			if row.Name != name || row.LaunchContext.PaneID == "" {
				continue
			}
			for _, pane := range snapshot.Panes {
				if pane.PaneID == row.LaunchContext.PaneID {
					return pane.PaneID, nil
				}
			}
		}
		timer := time.NewTimer(deps.poll)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return "", fmt.Errorf("forked agent %q did not appear with a pane within %s", name, timeout)
		case <-timer.C:
		}
	}
}

func refuse(w http.ResponseWriter, status int, short, detail string) {
	writeJSON(w, status, refusal{Error: short, Detail: detail})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func serveEvents(w http.ResponseWriter, r *http.Request, deps dependencies) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		refuse(w, http.StatusInternalServerError, "stream unavailable", "response writer does not support flushing")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	unreachable := make(map[string]bool)
	hcomRosterDown := false
	hcomEventsDown := false
	messageCh := make(chan hcomevents.Message)
	hcomHealthy := make(chan struct{})
	hcomState := make(chan error, 1)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		cursor := &hcomevents.Cursor{}
		for ctx.Err() == nil {
			err := deps.messages(ctx, cursor, func(message hcomevents.Message) error {
				select {
				case messageCh <- message:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}, func() error {
				select {
				case hcomHealthy <- struct{}{}:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				err = errors.New("hcom event subscription stopped")
			}
			select {
			case hcomState <- err:
			case <-ctx.Done():
				return
			}
			timer := time.NewTimer(deps.poll)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
	}()
	var previous []byte
	emit := func(eventType string, value any) bool {
		data, err := json.Marshal(value)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	emitFailure := func(err error) bool {
		var sourced sourceError
		source := "unknown"
		if errors.As(err, &sourced) {
			source = sourced.source
		}
		unreachable[source] = true
		return emit("substrate", substrate{Source: source, Status: "unreachable", Detail: err.Error()})
	}
	board, err := readBoard(deps)
	if err != nil {
		var sourced sourceError
		if errors.As(err, &sourced) && sourced.source == "hcom" {
			hcomRosterDown = true
		}
		if !emitFailure(err) {
			return
		}
	} else {
		previous, _ = json.Marshal(board)
		if !emit("fleet", board) {
			return
		}
	}

	ticker := time.NewTicker(deps.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case message := <-messageCh:
			if !emit("message", message) {
				return
			}
		case <-hcomHealthy:
			hcomEventsDown = false
			if unreachable["hcom"] && !hcomRosterDown {
				unreachable["hcom"] = false
				if !emit("substrate", substrate{Source: "hcom", Status: "recovered"}) {
					return
				}
			}
		case eventErr := <-hcomState:
			if eventErr != nil {
				hcomEventsDown = true
				if !unreachable["hcom"] && !emitFailure(sourceError{"hcom", eventErr}) {
					return
				}
			}
		case <-ticker.C:
			board, boardErr := readBoard(deps)
			if boardErr != nil {
				var sourced sourceError
				source := "unknown"
				if errors.As(boardErr, &sourced) {
					source = sourced.source
				}
				if source == "hcom" {
					hcomRosterDown = true
				}
				if !unreachable[source] && !emitFailure(boardErr) {
					return
				}
				continue
			}
			hcomRosterDown = false
			for _, source := range []string{"herdr", "hcom"} {
				if source == "hcom" && hcomEventsDown {
					continue
				}
				if unreachable[source] {
					unreachable[source] = false
					if !emit("substrate", substrate{Source: source, Status: "recovered"}) {
						return
					}
				}
			}
			encoded, _ := json.Marshal(board)
			if !bytes.Equal(previous, encoded) {
				previous = encoded
				if !emit("fleet", board) {
					return
				}
			}
		}
	}
}
