package cli

import (
	"runtime"

	"github.com/spf13/cobra"

	"sesh/internal/setup"
)

func newSetup() *cobra.Command {
	var storeURL string
	var force bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install (or reconfigure) the per-user shipper service to run this binary",
		Long: `Install (or re-install) the per-user sesh shipper service on this node.

Idempotent: re-run after upgrading the binary or changing the store URL. The
service pins the absolute path of the binary running this command; the store
URL is the ONLY coupling between a node and the store (tsnet mode is plain
http — the tailnet encrypts transport).

Linux : installs a systemd --user unit (sesh-ship.service) + a drop-in
        carrying SESH_STORE_URL, then enables and starts it. Reboot survival
        on no-login nodes additionally needs: loginctl enable-linger $USER
Darwin: renders the launchd plist into ~/Library/LaunchAgents and bootstraps
        it into the gui domain.

Config files sesh setup writes carry a provenance digest. A file that still
matches its digest is replaced on re-run (URL changes included); a file the
operator edited — or one predating the digest — is never overwritten
without --force.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setup.Run(setup.Options{
				StoreURL: storeURL,
				Force:    force,
				DryRun:   dryRun,
				OS:       runtime.GOOS,
				Out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&storeURL, "store-url", "", "store base URL, e.g. http://sesh.<tailnet>.ts.net:8765 (required)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing node-local config even when its provenance digest is broken or absent")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print every action and rendered file; write nothing")
	_ = cmd.MarkFlagRequired("store-url")
	return cmd
}
