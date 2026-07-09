package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"sesh/internal/ship"
)

func newStatus() *cobra.Command {
	var stateDir string
	var storeURL string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report shipper/store health, staleness, and quarantine state",
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if stateDir == "" {
				stateDir, err = ship.StateDir()
				if err != nil {
					return err
				}
			}
			if storeURL == "" {
				storeURL = os.Getenv("SESH_STORE_URL")
			}
			cursors, err := ship.LoadSnapshot(stateDir)
			if err != nil {
				return err
			}
			sort.Slice(cursors, func(i, j int) bool {
				return cursors[i].Identity().Key() < cursors[j].Identity().Key()
			})
			fmt.Fprintf(cmd.OutOrStdout(), "cursors: %d\n", len(cursors))
			poisoned := 0
			var newestAck time.Time
			for _, c := range cursors {
				if c.Poisoned {
					poisoned++
				}
				if c.LastAckAt.After(newestAck) {
					newestAck = c.LastAckAt
				}
				ack := "never"
				if !c.LastAckAt.IsZero() {
					ack = time.Since(c.LastAckAt).Round(time.Second).String() + " ago"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "- %s offset=%d last_ack=%s", c.Identity().Key(), c.Offset, ack)
				if c.Poisoned {
					fmt.Fprint(cmd.OutOrStdout(), " POISONED")
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			if newestAck.IsZero() {
				fmt.Fprintln(cmd.OutOrStdout(), "last_ack_age: never")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "last_ack_age: %s\n", time.Since(newestAck).Round(time.Second))
			}
			if storeURL != "" {
				if err := pingStore(cmd.Context(), storeURL); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "store: unreachable (%v)\n", err)
					return fmt.Errorf("store unreachable: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "store: reachable")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "store: not configured")
			}
			if poisoned > 0 {
				return fmt.Errorf("%d poisoned file(s)", poisoned)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "shipper state directory")
	cmd.Flags().StringVar(&storeURL, "store-url", "", "store base URL (env: SESH_STORE_URL)")
	return cmd
}

func pingStore(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
