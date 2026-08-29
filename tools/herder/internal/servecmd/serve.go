// Package servecmd exposes the live fleet web API.
package servecmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/fileindex"
	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/fileroots"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/hcommessage"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/repoctx"
	"ai-config/tools/herder/internal/webaction"
	"ai-config/tools/herder/internal/webidentity"
	"ai-config/tools/herder/internal/webui"
	"github.com/fsnotify/fsnotify"
)

const (
	DefaultPort      = 4400
	PollCadence      = 2 * time.Second
	HeartbeatCadence = 15 * time.Second
	webNoteStart     = "[HERDER_WEB_OPERATOR_NOTE_BEGIN]"
	webNoteEnd       = "[HERDER_WEB_OPERATOR_NOTE_END]"
)

type dependencies struct {
	buildIdentity        string
	snapshot             func() (herdrcli.Snapshot, error)
	paneProcessNames     func([]string) (map[string]string, error)
	worktrees            func([]herdrcli.Workspace) (map[string]string, error)
	roster               func() ([]hcomidentity.Row, error)
	stopped              func(string) (hcomidentity.Row, error)
	messages             func(context.Context, *hcomevents.Cursor, func(hcomevents.Message) error, func() error) error
	recentMessages       func(context.Context, int) ([]hcomevents.Message, error)
	entryEnd             func(hcomidentity.Row) (int64, error)
	entryTail            func(hcomidentity.Row, claudesession.Cursor, int) (claudesession.TailResult, error)
	agentQueueExclusions func(hcomidentity.Row, map[string]queueCandidate) (map[string]bool, error)
	agentVitals          func(hcomidentity.Row) (claudesession.Vitals, error)
	sender               func(context.Context, string) (string, error)
	send                 func(context.Context, string, string, string) error
	spawn                func(context.Context, []string) (webaction.Result, error)
	poll                 time.Duration
	heartbeat            time.Duration
	listeners            func(int) ([]net.Listener, []string, error)
	screens              func() (screenSource, error)
	configuredRoots      []string
	roots                func(context.Context, []string, []hcomidentity.Row) (fileroots.Set, error)
	fileResolver         fileresolver.Resolver
	fileWatcher          func() (*fsnotify.Watcher, error)
	fileWatcherDelta     func(int)
	repoContext          func(context.Context, string) (repoctx.Context, error)
	now                  func() time.Time
	audit                func(string, ...any)
	inputSerial          *paneInputSerial
}

var liveDependencies = dependencies{
	snapshot:             herdrcli.LiveSnapshot,
	paneProcessNames:     herdrcli.PaneProcessNames,
	worktrees:            herdrcli.WorktreeParents,
	roster:               hcomidentity.List,
	stopped:              hcomidentity.Stopped,
	messages:             hcomevents.Subscribe,
	recentMessages:       hcomevents.Recent,
	entryEnd:             entryTailEnd,
	entryTail:            entryTail,
	agentQueueExclusions: readQueueExclusions,
	agentVitals:          readAgentVitals,
	sender:               webidentity.Sender,
	send:                 hcommessage.SendRequest,
	spawn:                webaction.Spawn,
	poll:                 PollCadence,
	heartbeat:            HeartbeatCadence,
	listeners:            liveListeners,
	screens: func() (screenSource, error) {
		return herdrcli.NewLiveScreens()
	},
	roots:        buildRootSet,
	fileResolver: fileresolver.New(fileindex.New(fileindex.Options{})),
	fileWatcher:  fsnotify.NewWatcher,
	repoContext:  repoctx.Read,
	now:          time.Now,
	audit:        log.Printf,
	inputSerial:  &paneInputSerial{},
}

type paneInputSerial struct{ locks sync.Map }

func (s *paneInputSerial) lock(paneID string) func() {
	value, _ := s.locks.LoadOrStore(paneID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
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

type streamHello struct {
	BuildIdentity string `json:"buildIdentity"`
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
	Name          string                      `json:"name"`
	Tool          string                      `json:"tool"`
	HerdrStatus   string                      `json:"herdr_status"`
	BusStatus     string                      `json:"bus_status"`
	Gap           string                      `json:"gap"`
	Pane          *agentPane                  `json:"pane"`
	Directory     string                      `json:"directory,omitempty"`
	CWD           string                      `json:"cwd,omitempty"`
	Git           *repoctx.Git                `json:"git,omitempty"`
	SessionID     string                      `json:"session_id,omitempty"`
	ParentAgent   string                      `json:"parent_agent,omitempty"`
	LaunchContext hcomidentity.LaunchContext  `json:"launch_context"`
	Model         string                      `json:"model,omitempty"`
	ContextUsage  *claudesession.ContextUsage `json:"context_usage,omitempty"`
	Queued        []queuedMessage             `json:"queued,omitempty"`
}

type queuedMessage struct {
	ID       int64  `json:"id"`
	Sender   string `json:"sender"`
	Intent   string `json:"intent,omitempty"`
	Preview  string `json:"preview"`
	SentAt   string `json:"sent_at"`
	Operator bool   `json:"operator,omitempty"`
}

type transcriptReset struct {
	Agent string `json:"agent"`
}

type eventTranscript struct {
	initialized bool
	session     string
	offset      int64
	retired     *hcomidentity.Row
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

type viewerResponse struct {
	Viewer string `json:"viewer"`
}

type paneInputRequest struct {
	Text *string   `json:"text"`
	Keys *[]string `json:"keys"`
}

type paneInputResponse struct {
	Sent   bool   `json:"sent"`
	PaneID string `json:"pane_id"`
	Viewer string `json:"viewer"`
}

type spawnRequest struct {
	FromPane *string `json:"from_pane"`
	Shape    *string `json:"shape"`
	Tool     *string `json:"tool"`
	Tag      *string `json:"tag"`
	Prompt   *string `json:"prompt"`
	Branch   *string `json:"branch"`
}

var (
	tagPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	branchPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	errSenderCollision = errors.New("derived web sender collides with a bus agent")
)

func webMessage(sender, text string) string {
	note := fmt.Sprintf("%s\n[This message came from a web operator named %s via the fleet web view. They cannot receive hcom messages; do not reply with `hcom send`. Answer in your normal chat turn; they are watching the session transcript live.]\n%s", webNoteStart, sender, webNoteEnd)
	return note + "\n\n" + text
}

// Run parses and serves `herder serve` until the process is stopped.
func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("herder serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", DefaultPort, "TCP port for loopback and tailscale listeners")
	watch := fs.Bool("watch", false, "re-exec when the deployed herder build changes")
	var rootArgs rootFlags
	fs.Var(&rootArgs, "root", "additional readable root (repeatable; order controls resolve preference)")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "herder serve — expose the live fleet API.\n\nUsage:\n  herder serve [--port PORT] [--watch] [--root PATH ...]\n")
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
	configuredRoots, err := fileroots.CanonicalConfigured(rootArgs)
	if err != nil {
		fmt.Fprintf(stderr, "herder serve: invalid root: %v\n", err)
		return 2
	}
	ctx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	if *watch {
		config, err := newWatchConfig(os.Args)
		if err != nil {
			fmt.Fprintf(stderr, "herder serve: watch unavailable: %v\n", err)
			return 1
		}
		startWatch(ctx, config, stderr)
	}
	listeners, warnings, err := liveDependencies.listeners(*port)
	if err != nil {
		fmt.Fprintf(stderr, "herder serve: %v\n", err)
		return 1
	}
	for _, warning := range warnings {
		fmt.Fprintf(stderr, "herder serve: WARNING: %s\n", warning)
	}
	buildIdentity, err := runningBuildIdentity()
	if err != nil {
		for _, listener := range listeners {
			_ = listener.Close()
		}
		fmt.Fprintf(stderr, "herder serve: identify running build: %v\n", err)
		return 1
	}
	runtimeDependencies := liveDependencies
	runtimeDependencies.buildIdentity = buildIdentity
	runtimeDependencies.configuredRoots = configuredRoots
	return serve(listeners, newHandler(runtimeDependencies), stdout, stderr)
}

type rootFlags []string

func (r *rootFlags) String() string { return strings.Join(*r, ",") }

func (r *rootFlags) Set(value string) error {
	*r = append(*r, value)
	return nil
}

var cachedBuildName = regexp.MustCompile(`^herder-([0-9a-f]{16})$`)

func runningBuildIdentity() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	if match := cachedBuildName.FindStringSubmatch(filepath.Base(executable)); match != nil {
		return "source:" + match[1], nil
	}
	file, err := os.Open(executable)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "executable:" + hex.EncodeToString(hash.Sum(nil)), nil
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
	if deps.audit == nil {
		deps.audit = log.Printf
	}
	if deps.inputSerial == nil {
		deps.inputSerial = &paneInputSerial{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/fleet", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		board, err := readBoard(r.Context(), deps)
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
	mux.HandleFunc("/api/viewer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveViewer(w, r, deps)
	})
	mux.HandleFunc("/api/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveResolve(w, r, deps)
	})
	mux.HandleFunc("/api/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveFile(w, r, deps)
	})
	mux.HandleFunc("/api/files/tree", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveTree(w, r, deps)
	})
	mux.HandleFunc("/api/backlog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveBacklog(w, r, deps)
	})
	mux.HandleFunc("/api/git/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveGitStatus(w, r, deps)
	})
	mux.HandleFunc("/api/git/diff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveGitDiff(w, r, deps)
	})
	mux.HandleFunc("/api/git/log", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveGitLog(w, r, deps)
	})
	mux.HandleFunc("/api/git/file", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveGitFile(w, r, deps)
	})
	mux.HandleFunc("/api/agents/{busName}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		detail, err := readAgent(r.Context(), deps, r.PathValue("busName"))
		if err != nil {
			serveAgentReadError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
	mux.HandleFunc("/api/agents/{busName}/entries", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		serveEntries(w, r, deps, r.PathValue("busName"))
	})
	mux.HandleFunc("/api/agents/{busName}/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			refuse(w, http.StatusBadRequest, "bad request", "POST required")
			return
		}
		serveMessage(w, r, deps, r.PathValue("busName"))
	})
	mux.HandleFunc("/api/panes/{paneID}/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusBadRequest, "bad request", "GET required")
			return
		}
		servePaneHistory(w, r, deps, r.PathValue("paneID"))
	})
	mux.HandleFunc("/api/panes/{paneID}/input", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			refuse(w, http.StatusBadRequest, "bad request", "POST required")
			return
		}
		servePaneInput(w, r, deps, r.PathValue("paneID"))
	})
	mux.HandleFunc("/api/spawn", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			refuse(w, http.StatusBadRequest, "bad request", "POST required")
			return
		}
		serveSpawn(w, r, deps)
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

func readBoard(ctx context.Context, deps dependencies) (fleetview.Board, error) {
	snapshot, roster, err := readFleetInputs(deps)
	if err != nil {
		return fleetview.Board{}, err
	}
	return buildBoard(ctx, deps, snapshot, roster)
}

func buildBoard(ctx context.Context, deps dependencies, snapshot herdrcli.Snapshot, roster []hcomidentity.Row) (fleetview.Board, error) {
	parents, err := deps.worktrees(snapshot.Workspaces)
	if err != nil {
		return fleetview.Board{}, sourceError{"herdr", err}
	}
	if deps.paneProcessNames != nil {
		terminalPaneIDs := make([]string, 0, len(snapshot.Panes))
		for _, pane := range snapshot.Panes {
			if pane.Agent == "" && pane.AgentSession == "" {
				terminalPaneIDs = append(terminalPaneIDs, pane.PaneID)
			}
		}
		commands, processErr := deps.paneProcessNames(terminalPaneIDs)
		if processErr == nil {
			for index := range snapshot.Panes {
				snapshot.Panes[index].CurrentCommand = commands[snapshot.Panes[index].PaneID]
			}
		}
	}
	board := fleetview.Build(snapshot, roster, parents)
	workspaces := make(map[string]herdrcli.Workspace, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		workspaces[workspace.WorkspaceID] = workspace
	}
	for index := range board.Workspaces {
		source := workspaces[board.Workspaces[index].WorkspaceID]
		if source.Worktree == nil || source.Worktree.CheckoutPath == "" {
			continue
		}
		repository, contextErr := deps.repoContext(ctx, source.Worktree.CheckoutPath)
		if contextErr != nil {
			return fleetview.Board{}, sourceError{"git", contextErr}
		}
		board.Workspaces[index].CWD = repository.CWD
		board.Workspaces[index].Git = repository.Git
	}
	return board, nil
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
	return snapshot, hcomidentity.WithParents(roster), nil
}

var errUnknownAgent = errors.New("unknown agent")

func readAgent(ctx context.Context, deps dependencies, name string) (agentDetail, error) {
	row, retired, roster, err := resolveAgentEvidence(deps, name)
	if err != nil {
		return agentDetail{}, err
	}
	result := agentDetail{
		Name: name, Tool: row.Tool, HerdrStatus: "-", BusStatus: row.Status,
		Gap: "no visible pane", Directory: row.Directory, SessionID: row.SessionID,
		LaunchContext: row.LaunchContext,
		ParentAgent:   row.ParentAgent,
	}
	if row.Directory != "" {
		repository, contextErr := deps.repoContext(ctx, row.Directory)
		if contextErr != nil {
			return agentDetail{}, sourceError{"git", contextErr}
		}
		result.CWD = repository.CWD
		result.Git = repository.Git
	}
	vitals, err := deps.agentVitals(row)
	if err != nil {
		return agentDetail{}, err
	}
	result.Model = vitals.Model
	result.ContextUsage = vitals.ContextUsage
	// Queued is an optional proven fact. A bus/session read failure must not
	// degrade the otherwise useful detail response or invent delivery state.
	if !retired {
		if messages, messageErr := deps.recentMessages(ctx, 500); messageErr == nil {
			candidates := operatorQueueCandidates(name, row.BaseName, messages, roster)
			if excluded, entryErr := deps.agentQueueExclusions(row, candidates); entryErr == nil {
				result.Queued = diffQueuedMessages(messages, candidates, excluded)
			}
		}
	}
	if retired {
		return result, nil
	}
	snapshot, err := deps.snapshot()
	if err != nil {
		return agentDetail{}, sourceError{"herdr", err}
	}
	if err := fleetview.ValidateSnapshot(snapshot); err != nil {
		return agentDetail{}, sourceError{"herdr", fmt.Errorf("invalid session hierarchy: %w", err)}
	}
	resolvedPane := ""
	for _, row := range fleetview.JoinRows(snapshot, roster) {
		if row.Agent == name && row.BusStatus != "-" {
			result.Tool = row.Tool
			result.HerdrStatus = row.HerdrStatus
			result.BusStatus = row.BusStatus
			result.Gap = row.Gap
			resolvedPane = row.Pane
			break
		}
	}
	if resolvedPane != "" && resolvedPane != "-" {
		for _, pane := range snapshot.Panes {
			if pane.PaneID == resolvedPane {
				result.Pane = &agentPane{WorkspaceID: pane.WorkspaceID, TabID: pane.TabID, PaneID: pane.PaneID}
				break
			}
		}
	}
	return result, nil
}

// resolveAgentEvidence is deliberately live-first: a reused live name always
// wins over an older stopped record. Stopped hcom history is the only retired
// identity authority; a client-held session ID is never accepted as evidence.
func resolveAgentEvidence(deps dependencies, name string) (hcomidentity.Row, bool, []hcomidentity.Row, error) {
	roster, err := deps.roster()
	if err != nil {
		return hcomidentity.Row{}, false, nil, sourceError{"hcom", err}
	}
	if err := fleetview.ValidateRoster(roster); err != nil {
		return hcomidentity.Row{}, false, nil, sourceError{"hcom", fmt.Errorf("invalid roster: %w", err)}
	}
	roster = hcomidentity.WithParents(roster)
	for _, row := range roster {
		if row.Name == name {
			return row, false, roster, nil
		}
	}
	row, err := deps.stopped(name)
	if errors.Is(err, hcomidentity.ErrStoppedNotFound) {
		return hcomidentity.Row{}, false, roster, fmt.Errorf("%w: no live or retained session evidence for %q", errUnknownAgent, name)
	}
	if err != nil {
		return hcomidentity.Row{}, false, roster, sourceError{"hcom", err}
	}
	return row, true, roster, nil
}

func serveAgentReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnknownAgent) {
		refuse(w, http.StatusNotFound, "unknown agent", err.Error())
		return
	}
	refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
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
		_, stoppedErr := deps.stopped(name)
		if stoppedErr == nil {
			refuse(w, http.StatusConflict, "retired agent", fmt.Sprintf("agent %q is retired; its transcript is read-only", name))
			return
		}
		if !errors.Is(stoppedErr, hcomidentity.ErrStoppedNotFound) {
			refuse(w, http.StatusBadGateway, "substrate unreachable", stoppedErr.Error())
			return
		}
		refuse(w, http.StatusNotFound, "unknown agent", fmt.Sprintf("no live or retained session evidence for %q", name))
		return
	}
	var request messageRequest
	if err := decodeWriteBody(w, r, &request, false); err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if strings.TrimSpace(request.Text) == "" {
		refuse(w, http.StatusBadRequest, "bad request", "text must not be empty")
		return
	}
	sender, err := attributedSender(r, deps, roster)
	if err != nil {
		serveAttributionError(w, err)
		return
	}
	if err := deps.send(r.Context(), name, sender, webMessage(sender, request.Text)); err != nil {
		if errors.Is(err, hcommessage.ErrUnavailable) {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
			return
		}
		refuse(w, http.StatusConflict, "refused by substrate", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, messageResponse{Sent: true, To: name, From: sender, Intent: "request"})
}

func serveViewer(w http.ResponseWriter, r *http.Request, deps dependencies) {
	roster, err := deps.roster()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	sender, err := attributedSender(r, deps, roster)
	if err != nil {
		serveAttributionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewerResponse{Viewer: sender})
}

func servePaneHistory(w http.ResponseWriter, r *http.Request, deps dependencies, paneID string) {
	snapshot, err := deps.snapshot()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	found := false
	for _, pane := range snapshot.Panes {
		if pane.PaneID == paneID {
			found = true
			break
		}
	}
	if !found {
		refuse(w, http.StatusNotFound, "unknown pane", fmt.Sprintf("pane %q is not reported by Herdr", paneID))
		return
	}
	source, err := deps.screens()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	read, err := source.ReadHistory(paneID, maxHistoryLines)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if read.PaneID != paneID {
		refuse(w, http.StatusBadGateway, "substrate unreachable", fmt.Sprintf("Herdr returned pane %q for requested pane %q", read.PaneID, paneID))
		return
	}
	writeJSON(w, http.StatusOK, paneHistory{PaneID: paneID, Text: read.Text, Truncated: read.Truncated, FetchedAt: deps.now().UTC().Format(time.RFC3339Nano)})
}

func servePaneInput(w http.ResponseWriter, r *http.Request, deps dependencies, paneID string) {
	var request paneInputRequest
	if err := decodeWriteBodyLimit(w, r, &request, false, 8<<10); err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error()+"; terminal input is limited to 8 KiB")
		return
	}
	if (request.Text == nil) == (request.Keys == nil) {
		refuse(w, http.StatusBadRequest, "bad request", "exactly one of text or keys is required")
		return
	}
	if request.Text != nil && *request.Text == "" {
		refuse(w, http.StatusBadRequest, "bad request", "text must not be empty")
		return
	}
	if request.Keys != nil && len(*request.Keys) == 0 {
		refuse(w, http.StatusBadRequest, "bad request", "keys must not be empty")
		return
	}
	snapshot, err := deps.snapshot()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	found := false
	for _, pane := range snapshot.Panes {
		if pane.PaneID == paneID {
			found = true
			break
		}
	}
	if !found {
		refuse(w, http.StatusNotFound, "unknown pane", fmt.Sprintf("pane %q is not reported by Herdr", paneID))
		return
	}
	roster, err := deps.roster()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	viewer, err := attributedSender(r, deps, roster)
	if err != nil {
		serveAttributionError(w, err)
		return
	}
	input := herdrcli.PaneInput{PaneID: paneID}
	byteCount := 0
	if request.Text != nil {
		input.Text = *request.Text
		byteCount = len([]byte(*request.Text))
	}
	if request.Keys != nil {
		input.Keys = append([]string(nil), (*request.Keys)...)
		for _, key := range input.Keys {
			byteCount += len([]byte(key))
		}
	}
	deps.audit("terminal_input time=%s viewer=%s pane=%s bytes=%d", deps.now().UTC().Format(time.RFC3339Nano), viewer, paneID, byteCount)
	unlock := deps.inputSerial.lock(paneID)
	err = func() error {
		defer unlock()
		source, sourceErr := deps.screens()
		if sourceErr != nil {
			return sourceErr
		}
		return source.SendInput(input)
	}()
	if errors.Is(err, herdrcli.ErrPaneGone) {
		refuse(w, http.StatusConflict, "pane disappeared", err.Error())
		return
	}
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, paneInputResponse{Sent: true, PaneID: paneID, Viewer: viewer})
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

func attributedSender(r *http.Request, deps dependencies, roster []hcomidentity.Row) (string, error) {
	sender, err := deps.sender(r.Context(), r.RemoteAddr)
	if err != nil {
		return "", err
	}
	for _, row := range roster {
		if strings.EqualFold(row.Name, sender) {
			return "", fmt.Errorf("%w: derived sender %q is already a bus agent", errSenderCollision, sender)
		}
	}
	return sender, nil
}

func serveAttributionError(w http.ResponseWriter, err error) {
	if errors.Is(err, webidentity.ErrUnavailable) {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if errors.Is(err, errSenderCollision) {
		refuse(w, http.StatusConflict, "sender refused", err.Error())
		return
	}
	refuse(w, http.StatusConflict, "attribution required", err.Error())
}

func decodeWriteBody(w http.ResponseWriter, r *http.Request, target any, emptyOK bool) error {
	return decodeWriteBodyLimit(w, r, target, emptyOK, 64<<10)
}

func decodeWriteBodyLimit(w http.ResponseWriter, r *http.Request, target any, emptyOK bool, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
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

func refuse(w http.ResponseWriter, status int, short, detail string) {
	writeJSON(w, status, refusal{Error: short, Detail: detail})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func eventAgents(raw string) ([]string, error) {
	return eventSet(raw, "agents")
}

func eventSet(raw, parameter string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	seen := make(map[string]bool)
	agents := make([]string, 0)
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "\r\n") {
			return nil, fmt.Errorf("%s must be a comma-separated list of names", parameter)
		}
		if !seen[name] {
			seen[name] = true
			agents = append(agents, name)
		}
		if len(agents) > 100 {
			return nil, fmt.Errorf("%s accepts at most 100 names", parameter)
		}
	}
	return agents, nil
}

func serveEvents(w http.ResponseWriter, r *http.Request, deps dependencies) {
	agents, err := eventAgents(r.URL.Query().Get("agents"))
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	screens, err := eventSet(r.URL.Query().Get("screens"), "screens")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	focusedScreen, err := optionalQuery(r, "focused_screen")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if focusedScreen != "" {
		found := false
		for _, paneID := range screens {
			if paneID == focusedScreen {
				found = true
				break
			}
		}
		if !found {
			refuse(w, http.StatusBadRequest, "bad request", "focused_screen must name one of the requested screens")
			return
		}
	}
	watchesRaw, err := optionalQuery(r, "watches")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	watches, err := parseFileWatchRequests(watchesRaw)
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		refuse(w, http.StatusInternalServerError, "stream unavailable", "response writer does not support flushing")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	unreachable := make(map[string]bool)
	transcripts := make(map[string]*eventTranscript, len(agents))
	for _, name := range agents {
		transcripts[name] = &eventTranscript{}
	}
	hcomRosterDown := false
	hcomEventsDown := false
	messageCh := make(chan hcomevents.Message)
	hcomHealthy := make(chan struct{})
	hcomState := make(chan error, 1)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	fileWatches := startFileWatches(ctx, deps, watches)
	if fileWatches != nil {
		defer fileWatches.Close()
	}
	var fileChangeCh <-chan fileChangeFact
	if fileWatches != nil {
		fileChangeCh = fileWatches.Facts
	}
	var screenReader screenSource
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
	if !emit("hello", streamHello{BuildIdentity: deps.buildIdentity}) {
		return
	}
	if len(screens) > 0 {
		if source, sourceErr := deps.screens(); sourceErr == nil {
			screenReader = source
		}
	}
	screenStates := make(map[string]*eventScreen, len(screens))
	emitScreen := func(paneID string, frame screenFrame) bool {
		wire, encodeErr := encodeScreenEvent(paneID, frame)
		if encodeErr != nil {
			return false
		}
		if _, writeErr := w.Write(wire); writeErr != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	emitScreenUnavailable := func(paneID, detail string) bool {
		state := screenStates[paneID]
		if state == nil {
			state = &eventScreen{paneID: paneID}
			screenStates[paneID] = state
		}
		if state.status == "unavailable" && state.detail == detail {
			return true
		}
		state.status, state.detail, state.text, state.truncated, state.dirty = "unavailable", detail, "", false, false
		return emitScreen(paneID, screenFrame{PaneID: paneID, Status: "unavailable", Text: "", Detail: detail})
	}
	syncScreenRequests := func(panes map[string]screenPaneFact, force bool) bool {
		for _, paneID := range screens {
			fact, visible := panes[paneID]
			if !visible {
				if state := screenStates[paneID]; state != nil {
					state.visible = false
				}
				if !emitScreenUnavailable(paneID, fmt.Sprintf("pane %q is not reported by Herdr", paneID)) {
					return false
				}
				continue
			}
			if screenReader == nil {
				if !emitScreenUnavailable(paneID, "Herdr screen reader is unavailable") {
					return false
				}
				continue
			}
			state := screenStates[paneID]
			if state == nil {
				state = &eventScreen{paneID: paneID}
				screenStates[paneID] = state
			}
			state.visible = true
			if force || state.status != "available" || state.revision != fact.revision || state.cols != fact.cols || state.rows != fact.rows {
				state.dirty = true
			}
			state.revision, state.cols, state.rows = fact.revision, fact.cols, fact.rows
		}
		return true
	}
	flushScreens := func(now time.Time, onlyFocused bool) bool {
		for _, paneID := range screens {
			if onlyFocused != (paneID == focusedScreen) {
				continue
			}
			state := screenStates[paneID]
			if state == nil || !state.dirty || screenReader == nil {
				continue
			}
			cadence := backgroundScreenCadence
			if paneID == focusedScreen {
				cadence = focusedScreenCadence
			}
			if state.lastPollNS != 0 && now.UnixNano()-state.lastPollNS < cadence.Nanoseconds() {
				continue
			}
			state.dirty = false
			state.lastPollNS = now.UnixNano()
			read, readErr := screenReader.ReadVisible(paneID)
			if readErr != nil {
				if !emitScreenUnavailable(paneID, readErr.Error()) {
					return false
				}
				continue
			}
			if read.PaneID != paneID {
				if !emitScreenUnavailable(paneID, fmt.Sprintf("Herdr returned pane %q for requested pane %q", read.PaneID, paneID)) {
					return false
				}
				continue
			}
			if state.status == "available" && state.text == read.Text && state.truncated == read.Truncated && state.emittedCols == state.cols && state.emittedRows == state.rows {
				continue
			}
			frame := screenFrame{PaneID: paneID, Revision: state.revision, Status: "available", Text: read.Text, Truncated: read.Truncated, Cols: state.cols, Rows: state.rows}
			if !emitScreen(paneID, frame) {
				return false
			}
			state.status, state.detail, state.text, state.truncated, state.emittedCols, state.emittedRows = "available", "", read.Text, read.Truncated, state.cols, state.rows
		}
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
	emitTranscriptFailure := func(name string, err error) bool {
		source := "transcript:" + name
		if unreachable[source] {
			return true
		}
		unreachable[source] = true
		return emit("substrate", substrate{Source: source, Status: "unreachable", Detail: err.Error()})
	}
	syncTranscripts := func(roster []hcomidentity.Row, initial bool) bool {
		pendingFailures := make([]substrate, 0)
		rows := make(map[string]hcomidentity.Row, len(roster))
		for _, row := range roster {
			rows[row.Name] = row
		}
		for _, name := range agents {
			state := transcripts[name]
			row, exists := rows[name]
			if !exists {
				if state.retired != nil {
					row = *state.retired
					exists = true
				} else {
					stopped, stoppedErr := deps.stopped(name)
					if stoppedErr == nil {
						state.retired = &stopped
						row = stopped
						exists = true
					} else {
						detail := fmt.Sprintf("no live or retained session evidence for %q", name)
						if !errors.Is(stoppedErr, hcomidentity.ErrStoppedNotFound) {
							detail = stoppedErr.Error()
						}
						err := errors.New(detail)
						if initial {
							source := "transcript:" + name
							unreachable[source] = true
							pendingFailures = append(pendingFailures, substrate{Source: source, Status: "unreachable", Detail: err.Error()})
						} else if !emitTranscriptFailure(name, err) {
							return false
						}
						continue
					}
				}
			}
			if !exists {
				continue
			}
			if _, live := rows[name]; live {
				state.retired = nil
			}
			transcriptID := entryTranscriptID(row)
			if !state.initialized || state.session != transcriptID {
				end, endErr := deps.entryEnd(row)
				if endErr != nil {
					if initial {
						source := "transcript:" + name
						unreachable[source] = true
						pendingFailures = append(pendingFailures, substrate{Source: source, Status: "unreachable", Detail: endErr.Error()})
					} else if !emitTranscriptFailure(name, endErr) {
						return false
					}
					continue
				}
				state.initialized = true
				state.session = transcriptID
				state.offset = end
				if !initial && !emit("rewindow", transcriptReset{Agent: name}) {
					return false
				}
			}
			tail, tailErr := deps.entryTail(row, claudesession.Cursor{SessionID: state.session, Offset: state.offset}, maxEntryWindow)
			if tailErr != nil {
				if initial {
					source := "transcript:" + name
					unreachable[source] = true
					pendingFailures = append(pendingFailures, substrate{Source: source, Status: "unreachable", Detail: tailErr.Error()})
				} else if !emitTranscriptFailure(name, tailErr) {
					return false
				}
				continue
			}
			source := "transcript:" + name
			if unreachable[source] {
				unreachable[source] = false
				if !emit("substrate", substrate{Source: source, Status: "recovered"}) {
					return false
				}
			}
			if tail.Reset != nil {
				end, endErr := deps.entryEnd(row)
				if endErr != nil {
					if !emitTranscriptFailure(name, endErr) {
						return false
					}
					continue
				}
				state.session = transcriptID
				state.offset = end
				if !emit("rewindow", transcriptReset{Agent: name}) {
					return false
				}
				continue
			}
			for _, entry := range serializeEntries(tail.Read.Entries) {
				if !emit("entry:"+name, entry) {
					return false
				}
			}
			state.offset = tail.Cursor.Offset
		}
		for _, failure := range pendingFailures {
			if !emit("substrate", failure) {
				return false
			}
		}
		return true
	}
	readEventBoard := func() (fleetview.Board, []hcomidentity.Row, map[string]screenPaneFact, error) {
		snapshot, roster, readErr := readFleetInputs(deps)
		if readErr != nil {
			return fleetview.Board{}, nil, nil, readErr
		}
		board, readErr := buildBoard(r.Context(), deps, snapshot, roster)
		return board, roster, paneScreenFacts(snapshot), readErr
	}
	board, roster, panes, err := readEventBoard()
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
		if !syncTranscripts(roster, true) {
			return
		}
		if !emit("fleet", board) {
			return
		}
		if !syncScreenRequests(panes, true) || !flushScreens(time.Now(), false) || (focusedScreen != "" && !flushScreens(time.Now(), true)) {
			return
		}
	}

	ticker := time.NewTicker(deps.poll)
	defer ticker.Stop()
	heartbeat := time.NewTicker(deps.heartbeat)
	defer heartbeat.Stop()
	var backgroundScreenTick <-chan time.Time
	var backgroundScreenTicker *time.Ticker
	var focusedScreenTick <-chan time.Time
	var focusedScreenTicker *time.Ticker
	if len(screens) > 0 {
		backgroundScreenTicker = time.NewTicker(backgroundScreenCadence)
		backgroundScreenTick = backgroundScreenTicker.C
		defer backgroundScreenTicker.Stop()
	}
	if focusedScreen != "" {
		focusedScreenTicker = time.NewTicker(focusedScreenCadence)
		focusedScreenTick = focusedScreenTicker.C
		defer focusedScreenTicker.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case message := <-messageCh:
			if !emit("message", message) {
				return
			}
		case fact := <-fileChangeCh:
			if !emit("file-change", fact) {
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
		case now := <-backgroundScreenTick:
			for _, paneID := range screens {
				if paneID != focusedScreen {
					if state := screenStates[paneID]; state != nil && state.visible {
						state.dirty = true
					}
				}
			}
			if !flushScreens(now, false) {
				return
			}
		case now := <-focusedScreenTick:
			if state := screenStates[focusedScreen]; state != nil && state.visible {
				state.dirty = true
			}
			if !flushScreens(now, true) {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\nevent: ping\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			board, roster, panes, boardErr := readEventBoard()
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
			if !syncTranscripts(roster, false) {
				return
			}
			if screenReader == nil && len(screens) > 0 {
				if source, sourceErr := deps.screens(); sourceErr == nil {
					screenReader = source
				}
			}
			if !syncScreenRequests(panes, true) || !flushScreens(time.Now(), false) || (focusedScreen != "" && !flushScreens(time.Now(), true)) {
				return
			}
		}
	}
}
