// Package cli wires the sesh command tree. Every subcommand is a stub until
// its owning unit lands (plan 2026-07-09-001, U3 onward); stubs report
// not-implemented and exit nonzero so nothing can script against them early.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
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
		stub("serve", "Run the central store: byte-range ingest, index, and team surface"),
		stub("reindex", "Rebuild the disposable index from the durable mirror"),
		stub("status", "Report shipper/store health, staleness, and quarantine state"),
		newAdmin(),
	)
	return root
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
