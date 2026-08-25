// Package servecmd exposes the read-only fleet web API.
package servecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
)

const (
	DefaultPort = 4400
	PollCadence = 2 * time.Second
)

type dependencies struct {
	snapshot  func() (herdrcli.Snapshot, error)
	roster    func() ([]hcomidentity.Row, error)
	messages  func(context.Context, *hcomevents.Cursor, func(hcomevents.Message) error, func() error) error
	poll      time.Duration
	listeners func(int) ([]net.Listener, []string, error)
}

var liveDependencies = dependencies{
	snapshot:  herdrcli.LiveSnapshot,
	roster:    hcomidentity.List,
	messages:  hcomevents.Subscribe,
	poll:      PollCadence,
	listeners: liveListeners,
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fleet" && r.URL.Path != "/api/events" {
			refuse(w, http.StatusNotFound, "not found", "unknown endpoint")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func readBoard(deps dependencies) (fleetview.Board, error) {
	snapshot, err := deps.snapshot()
	if err != nil {
		return fleetview.Board{}, sourceError{"herdr", err}
	}
	if err := fleetview.ValidateSnapshot(snapshot); err != nil {
		return fleetview.Board{}, sourceError{"herdr", fmt.Errorf("invalid session hierarchy: %w", err)}
	}
	roster, err := deps.roster()
	if err != nil {
		return fleetview.Board{}, sourceError{"hcom", err}
	}
	if err := fleetview.ValidateRoster(roster); err != nil {
		return fleetview.Board{}, sourceError{"hcom", fmt.Errorf("invalid roster: %w", err)}
	}
	return fleetview.Build(snapshot, roster), nil
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
