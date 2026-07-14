// Package cli wires the sesh command tree.
//
// The package registers only the fleet-client commands (ship, status, setup,
// update, version) plus error stubs for the store-side command names, so the
// client entry point (./cmd/sesh) never links the store, index, or surface
// packages — that dependency cut is what keeps the fleet client artifact
// slim (no tsnet, no sqlite; tests/check-client-slim.sh gates it). The store
// entry point (./cmd/sesh-store) passes the real store commands into Execute.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"sesh/internal/buildinfo"
)

// Execute runs the sesh command tree and returns its error, if any.
// main translates a non-nil error into an exit code (see ExitCode).
// storeCommands, when given, are registered in place of the store stubs;
// only the store entry point supplies them.
func Execute(storeCommands ...*cobra.Command) error {
	// SESH_DEBUG turns on debug-level logging (per-phase serving and index
	// timing) without a config change; it is the supported way to see where
	// store time goes on a live node.
	if os.Getenv("SESH_DEBUG") != "" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}
	return newRoot(storeCommands...).Execute()
}

func newRoot(storeCommands ...*cobra.Command) *cobra.Command {
	root := &cobra.Command{
		Use:          "sesh",
		Short:        "sesh mirrors Claude Code and Codex CLI session transcripts to a central store",
		SilenceUsage: true,
	}
	if len(storeCommands) == 0 {
		storeCommands = storeStubs()
	}
	root.AddCommand(
		&cobra.Command{
			Use:   "version",
			Short: "Print the build version",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, _ []string) {
				fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Version)
			},
		},
		newShip(),
		newStatus(),
		newSetup(),
		newUpdate(),
	)
	root.AddCommand(storeCommands...)
	return root
}

// storeStubs holds the store-side command names in the client tree so an
// operator who reaches for them on a fleet node gets one clear line naming
// the artifact that has them, instead of "unknown command". Flag parsing is
// disabled so any invocation shape (flags, subcommands) reaches the error
// rather than dying on an unknown flag first.
func storeStubs() []*cobra.Command {
	stub := func(use, short string) *cobra.Command {
		return &cobra.Command{
			Use:                use,
			Short:              short + " (sesh-store binary only)",
			Args:               cobra.ArbitraryArgs,
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return fmt.Errorf("sesh %s: store-only command not in this fleet client build — use the sesh-store binary (built from ./cmd/sesh-store; deployed by `just deploy-store`)", use)
			},
		}
	}
	return []*cobra.Command{
		stub("serve", "Run the central store"),
		stub("reindex", "Rebuild the store index"),
		stub("admin", "Administrative operations on the store"),
	}
}
