// Package cli wires the sesh command tree.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"sesh/internal/index"
	"sesh/internal/store"
	"sesh/internal/surface"
)

// Execute runs the sesh command tree and returns its error, if any.
// main translates a non-nil error into exit code 1.
func Execute() error {
	return newRoot().Execute()
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:          "sesh",
		Short:        "sesh mirrors Claude Code and Codex CLI session transcripts to a central store",
		SilenceUsage: true,
	}
	root.AddCommand(
		newShip(),
		newServe(),
		newReindex(),
		newStatus(),
		newAdmin(),
	)
	return root
}

func newServe() *cobra.Command {
	var addr string
	var surfaceAddr string
	var dataDir string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the central store: byte-range ingest, index, and team surface",
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if dataDir == "" {
				dataDir, err = defaultStoreDir()
				if err != nil {
					return err
				}
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
			st, err := store.Open(cmd.Context(), store.Config{
				Dir:    dataDir,
				Logger: slog.Default(),
			})
			if err != nil {
				_ = l.Close()
				_ = sl.Close()
				return err
			}
			defer st.Close()
			idx, err := index.New(cmd.Context(), st.DB(), st.MirrorPath)
			if err != nil {
				_ = l.Close()
				_ = sl.Close()
				return err
			}
			startIndexConsumer(cmd.Context(), st, idx, slog.Default())
			go func() {
				srv := surface.New(surface.NewSQLStore(st.DB(), st.MirrorPath))
				if err := http.Serve(sl, srv); err != nil {
					slog.Default().Error("surface listener stopped", "error", err)
				}
			}()
			return st.Serve(l)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "loopback address for the store HTTP listener")
	cmd.Flags().StringVar(&surfaceAddr, "surface-addr", "127.0.0.1:8766", "loopback address for the read-only surface listener")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "store data directory")
	return cmd
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

func startIndexConsumer(ctx context.Context, st *store.Store, idx *index.Indexer, logger *slog.Logger) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-st.AppendEvents():
				if err := st.WithWriteLock(func() error {
					return idx.ProcessAppend(ctx, ev)
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
		}
	}()
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
