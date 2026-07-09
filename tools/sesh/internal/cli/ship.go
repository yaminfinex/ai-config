package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"syscall"

	"github.com/spf13/cobra"

	"sesh/internal/ship"
)

// newShip wires `sesh ship`, the per-user shipper daemon. Its whole config
// surface is the store URL (flag or SESH_STORE_URL) — the store's location is
// a deployment-time value, nothing else is worth deciding on a node.
func newShip() *cobra.Command {
	var storeURL string
	cmd := &cobra.Command{
		Use:   "ship",
		Short: "Run the per-user shipper: discover, tail, and mirror local session files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if storeURL == "" {
				storeURL = os.Getenv("SESH_STORE_URL")
			}
			if storeURL == "" {
				return fmt.Errorf("no store URL: pass --store-url or set SESH_STORE_URL")
			}
			stateDir, err := ship.StateDir()
			if err != nil {
				return err
			}
			reg, err := ship.OpenRegistry(stateDir)
			if err != nil {
				return err
			}
			defer reg.Close()

			hostname, err := os.Hostname()
			if err != nil {
				return err
			}
			u, err := user.Current()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			s := &ship.Shipper{
				Registry: reg,
				Client:   &ship.Client{BaseURL: storeURL, Hostname: hostname, OSUser: u.Username},
				Roots:    ship.DefaultRoots(home),
			}
			err = s.Run(ctx)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
	cmd.Flags().StringVar(&storeURL, "store-url", "", "store base URL (env: SESH_STORE_URL)")
	return cmd
}
