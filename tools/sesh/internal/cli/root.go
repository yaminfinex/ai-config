// Package cli wires the sesh command tree. Every subcommand is a stub until
// its owning unit lands (plan 2026-07-09-001, U3 onward); stubs report
// not-implemented and exit nonzero so nothing can script against them early.
package cli

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"sesh/internal/store"
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
		stub("ship", "Run the per-user shipper: discover, tail, and mirror local session files"),
		newServe(),
		stub("reindex", "Rebuild the disposable index from the durable mirror"),
		stub("status", "Report shipper/store health, staleness, and quarantine state"),
		newAdmin(),
	)
	return root
}

func newServe() *cobra.Command {
	var addr string
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
			l, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			host, _, err := net.SplitHostPort(l.Addr().String())
			if err != nil {
				_ = l.Close()
				return err
			}
			if !net.ParseIP(host).IsLoopback() {
				_ = l.Close()
				return fmt.Errorf("sesh serve: ingest listener must bind loopback before M4, got %s", l.Addr())
			}
			st, err := store.Open(cmd.Context(), store.Config{
				Dir:    dataDir,
				Logger: slog.Default(),
			})
			if err != nil {
				_ = l.Close()
				return err
			}
			defer st.Close()
			return st.Serve(l)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "loopback address for the store HTTP listener")
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

func newAdmin() *cobra.Command {
	admin := &cobra.Command{
		Use:   "admin",
		Short: "Administrative operations on the store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%s: missing subcommand", cmd.CommandPath())
		},
	}
	admin.AddCommand(
		stub("drop-file", "Drop a mirrored file from the store (redaction path)"),
	)
	return admin
}

func stub(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%s: not implemented", cmd.CommandPath())
		},
	}
}
