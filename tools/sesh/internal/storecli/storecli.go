// Package storecli wires the store-side commands (serve, reindex, admin)
// into a sesh command tree. It is linked ONLY by the store entry point
// (./cmd/sesh-store): keeping these commands — and through them the store,
// index, and surface packages with their tsnet and sqlite dependency trees —
// out of the fleet client is what keeps the client artifact slim. The client
// entry point (./cmd/sesh) registers error stubs for these command names
// instead; tests/check-client-slim.sh gates the resulting dependency graphs.
package storecli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"sesh/internal/index"
	"sesh/internal/store"
	"sesh/internal/surface"
	"sesh/internal/wire"
)

// Commands returns the store-side command set for cli.Execute.
func Commands() []*cobra.Command {
	return []*cobra.Command{
		newServe(),
		newReindex(),
		newAdmin(),
	}
}

func newServe() *cobra.Command {
	var addr string
	var surfaceAddr string
	var dataDir string
	var tsnetMode bool
	var tsnetHostname string
	var tsnetDir string
	var tsnetAuthKey string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the central store: byte-range ingest, index, and team surface",
		RunE: func(cmd *cobra.Command, args []string) error {
			serveCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			var err error
			if dataDir == "" {
				dataDir, err = defaultStoreDir()
				if err != nil {
					return err
				}
			}
			st, err := store.Open(serveCtx, store.Config{
				Dir:    dataDir,
				Logger: slog.Default(),
			})
			if err != nil {
				return err
			}
			defer st.Close()
			idx, err := index.New(serveCtx, st.DB(), st.MirrorPath)
			if err != nil {
				return err
			}
			consumer := startIndexConsumer(cmd.Context(), st, idx, slog.Default())
			defer func() { _ = consumer.StopAndWait() }()
			surfaceHandler, surfaceStore := newSurfaceHandler(st)
			// Deferred after st.Close, so it runs first: the projection
			// refresh goroutine is cancelled and drained before the pool it
			// reads shuts down.
			defer surfaceStore.Close()
			if tsnetMode {
				err = serveTSNet(serveCtx, st.Handler(), surfaceHandler, dataDir, addr, surfaceAddr, tsnetHostname, tsnetDir, tsnetAuthKey)
				if consumerErr := consumer.StopAndWait(); consumerErr != nil {
					return errors.Join(err, consumerErr)
				}
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
			l, err := listenLoopback(addr, "ingest")
			if err != nil {
				return err
			}
			// The read-only surface gets its own listener so the M2
			// exposure posture (proxy the read port, keep ingest loopback)
			// stays a deployment concern, not a code change (U8/R19).
			sl, err := listenLoopback(surfaceAddr, "surface")
			if err != nil {
				_ = l.Close()
				return err
			}
			err = serveHTTP(serveCtx,
				httpEndpoint{listener: l, handler: st.Handler()},
				httpEndpoint{listener: sl, handler: surfaceHandler},
			)
			if consumerErr := consumer.StopAndWait(); consumerErr != nil {
				return errors.Join(err, consumerErr)
			}
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "loopback address for the store HTTP listener")
	cmd.Flags().StringVar(&surfaceAddr, "surface-addr", "127.0.0.1:8766", "loopback address for the read-only surface listener")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "store data directory")
	cmd.Flags().BoolVar(&tsnetMode, "tsnet", false, "serve ingest and read listeners on tsnet with WhoIs grant checks")
	cmd.Flags().StringVar(&tsnetHostname, "tsnet-hostname", "sesh", "tsnet node hostname")
	cmd.Flags().StringVar(&tsnetDir, "tsnet-dir", "", "tsnet state directory; default is <data-dir>/tsnet")
	cmd.Flags().StringVar(&tsnetAuthKey, "tsnet-auth-key", "", "tsnet auth key; empty lets tsnet use TS_AUTHKEY or stored state")
	return cmd
}

// newSurfaceHandler wires the surface over the store's read-only pool: WAL
// readers run concurrently with the writer, so page loads never queue behind
// ingest/index append transactions on the single write connection (the
// measured remote-TTFB pathology; the regression gate holds a write
// transaction open and asserts surface reads still complete). The returned
// SQLStore owns the projection's background refresh goroutine; close it
// before closing the store whose pool it reads. Surface degradation events
// log through the process-default slog logger (surface.New's default), the
// same stderr → journald path as the timing and rebuild lines.
func newSurfaceHandler(st *store.Store) (http.Handler, *surface.SQLStore) {
	surfaceStore := surface.NewSQLStore(st.ReadDB(), st.MirrorPath)
	return surface.New(surfaceStore), surfaceStore
}

type tsnetServer interface {
	Listen(network, addr string) (net.Listener, error)
	WhoIs(context.Context, string) (store.WhoIsResult, error)
	Close() error
}

type tsnetServePlan struct {
	ingestAddr     string
	surfaceAddr    string
	ingestHandler  http.Handler
	surfaceHandler http.Handler
}

func newTSNetServePlan(ts tsnetServer, ingestHandler, surfaceHandler http.Handler, addr, surfaceAddr string) tsnetServePlan {
	// Route-scoped auth on the ingest listener (design §3): the distribution
	// surface (/install.sh, /releases/) admits EITHER verb so read-only
	// principals can install and update, while wire ingest stays ship-only.
	// No-verb callers are denied on every route.
	shipOnly := store.AuthHandler(ingestHandler, ts.WhoIs, store.CapabilityShip)
	shipOrRead := store.AuthHandlerAnyOf(ingestHandler, ts.WhoIs, store.CapabilityShip, store.CapabilityRead)
	ingest := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if store.IsDistributionPath(r.URL.Path) {
			shipOrRead.ServeHTTP(w, r)
			return
		}
		shipOnly.ServeHTTP(w, r)
	})
	return tsnetServePlan{
		ingestAddr:     tsnetListenAddr(addr),
		surfaceAddr:    tsnetListenAddr(surfaceAddr),
		ingestHandler:  ingest,
		surfaceHandler: store.AuthHandler(surfaceHandler, ts.WhoIs, store.CapabilityRead),
	}
}

func serveTSNet(ctx context.Context, ingestHandler, surfaceHandler http.Handler, dataDir, addr, surfaceAddr, hostname, tsnetDir, authKey string) error {
	if tsnetDir == "" {
		tsnetDir = filepath.Join(dataDir, "tsnet")
	}
	ts := store.NewTSNetServer(store.TSNetOptions{
		Hostname: hostname,
		Dir:      tsnetDir,
		AuthKey:  authKey,
	})
	defer ts.Close()
	plan := newTSNetServePlan(ts, ingestHandler, surfaceHandler, addr, surfaceAddr)
	l, err := ts.Listen("tcp", plan.ingestAddr)
	if err != nil {
		return err
	}
	sl, err := ts.Listen("tcp", plan.surfaceAddr)
	if err != nil {
		_ = l.Close()
		return err
	}
	return serveHTTP(ctx,
		httpEndpoint{listener: l, handler: plan.ingestHandler},
		httpEndpoint{listener: sl, handler: plan.surfaceHandler},
	)
}

const serveShutdownTimeout = 10 * time.Second

type httpEndpoint struct {
	listener net.Listener
	handler  http.Handler
}

// timedHandler logs one debug line per served request: normalized route
// class, status, and full server-side duration (auth + handler + response
// write) — nothing else. With SESH_DEBUG set this is the first stop for
// "where does request time go" on a live store. Paths are collapsed to a
// route class so session and file identities never persist in the journal
// (transcripts are exactly what sesh ships; identifiers in logs would leak
// corpus into a different retention domain).
func timedHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		h.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		slog.Debug("http request",
			"method", r.Method, "route", routeClass(r.URL.Path),
			"status", status, "duration", time.Since(start))
	})
}

// statusRecorder captures the first status code written; it forwards Flush
// so streaming handlers keep working behind the middleware.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	if sr.status == 0 {
		sr.status = code
	}
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// routeClassFixed is the allowlist of identifier-free fixed routes that may
// appear verbatim in debug logs. Everything else is either a known
// parameterized template or collapses to "other": the raw path is
// client-supplied input and must never reach the journal.
var routeClassFixed = map[string]bool{
	"/":                      true,
	"/nodes":                 true,
	"/sessions":              true,
	"/fragments/recency":     true,
	"/install.sh":            true,
	wire.APIRoot + "/health": true,
	wire.APIRoot + "/nodes":  true,
}

// routeClass maps a request path to a stable, identifier-free label for
// debug logs: parameterized routes collapse to their template, allowlisted
// fixed routes pass through, and anything unknown is "other".
func routeClass(p string) string {
	switch {
	case strings.HasPrefix(p, wire.APIRoot+"/files/"):
		return wire.APIRoot + "/files/*"
	case strings.HasPrefix(p, "/s/"):
		return "/s/*"
	case strings.HasPrefix(p, "/releases/"):
		return "/releases/*"
	case strings.HasPrefix(p, "/assets/"):
		return "/assets/*"
	case routeClassFixed[p]:
		return p
	default:
		return "other"
	}
}

func serveHTTP(ctx context.Context, endpoints ...httpEndpoint) error {
	servers := make([]*http.Server, len(endpoints))
	errCh := make(chan error, len(endpoints))
	for i, endpoint := range endpoints {
		servers[i] = &http.Server{Handler: timedHandler(endpoint.handler)}
		go func(server *http.Server, listener net.Listener) {
			errCh <- server.Serve(listener)
		}(servers[i], endpoint.listener)
	}

	completed := 0
	var serveErr error
	select {
	case serveErr = <-errCh:
		completed = 1
	case <-ctx.Done():
		serveErr = ctx.Err()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), serveShutdownTimeout)
	defer cancel()
	shutdownErrs := make(chan error, len(servers))
	var shutdowns sync.WaitGroup
	for _, server := range servers {
		shutdowns.Add(1)
		go func(server *http.Server) {
			defer shutdowns.Done()
			if err := server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
				shutdownErrs <- err
			}
		}(server)
	}
	shutdowns.Wait()
	close(shutdownErrs)
	for err := range shutdownErrs {
		if err != nil {
			return fmt.Errorf("shut down HTTP listeners: %w", err)
		}
	}
	for completed < len(servers) {
		<-errCh
		completed++
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return serveErr
}

func tsnetListenAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return addr
	}
	return ":" + port
}

// listenLoopback binds addr and enforces the pre-M4 posture: every listener
// stays loopback until tsnet auth lands (U11).
func listenLoopback(addr, name string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	if !net.ParseIP(host).IsLoopback() {
		_ = l.Close()
		return nil, fmt.Errorf("sesh serve: %s listener must bind loopback before M4, got %s", name, l.Addr())
	}
	return l, nil
}

type indexConsumer struct {
	stop         chan struct{}
	done         chan struct{}
	stopOnce     sync.Once
	waitOnce     sync.Once
	waitErr      error
	drainTimeout time.Duration
	store        *store.Store
}

func startIndexConsumer(ctx context.Context, st *store.Store, idx *index.Indexer, logger *slog.Logger) *indexConsumer {
	consumer := &indexConsumer{
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
		drainTimeout: serveShutdownTimeout,
		store:        st,
	}
	workCtx := context.WithoutCancel(ctx)
	process := func(ev wire.AppendEvent) {
		if err := st.WithWriteLock(func() error {
			return idx.ProcessAppend(workCtx, ev)
		}); err != nil {
			logger.Error("append index failed",
				"error", err,
				"tool", ev.Tool,
				"session_id", ev.WireSessionID,
				"file_uuid", ev.FileUUID,
				"generation", ev.Generation,
			)
		}
	}
	go func() {
		defer close(consumer.done)
		for {
			select {
			case <-consumer.stop:
				for {
					select {
					case ev := <-st.AppendEvents():
						process(ev)
					default:
						return
					}
				}
			case ev := <-st.AppendEvents():
				process(ev)
			}
		}
	}()
	return consumer
}

func (consumer *indexConsumer) StopAndWait() error {
	consumer.stopOnce.Do(func() { close(consumer.stop) })
	consumer.waitOnce.Do(func() {
		timer := time.NewTimer(consumer.drainTimeout)
		defer timer.Stop()
		select {
		case <-consumer.done:
		case <-timer.C:
			consumer.waitErr = errors.Join(
				errors.New("timed out draining index consumer"),
				consumer.markBufferedDirty(),
			)
		}
	})
	return consumer.waitErr
}

func (consumer *indexConsumer) markBufferedDirty() error {
	ctx, cancel := context.WithTimeout(context.Background(), serveShutdownTimeout)
	defer cancel()
	var err error
	for {
		select {
		case ev := <-consumer.store.AppendEvents():
			err = errors.Join(err, consumer.store.MarkDirtyForReindex(ctx, ev))
		default:
			return err
		}
	}
}

func newReindex() *cobra.Command {
	var dataDir string
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the disposable index from the durable mirror",
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if dataDir == "" {
				dataDir, err = defaultStoreDir()
				if err != nil {
					return err
				}
			}
			st, err := store.Open(cmd.Context(), store.Config{
				Dir:    dataDir,
				Logger: slog.Default(),
			})
			if err != nil {
				return err
			}
			defer st.Close()
			idx, err := index.New(cmd.Context(), st.DB(), st.MirrorPath)
			if err != nil {
				return err
			}
			return idx.Reindex(cmd.Context())
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "store data directory")
	return cmd
}

func defaultStoreDir() (string, error) {
	if dir := os.Getenv("SESH_STORE_DIR"); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "sesh", "store"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "sesh", "store"), nil
}
